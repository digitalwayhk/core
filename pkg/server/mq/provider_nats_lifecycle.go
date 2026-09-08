package mq

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const natsLifecycleGeneration = "1"

var natsLifecycleOwnerSequence atomic.Uint64

func (*NATSJetStreamProvider) LifecycleCapabilities() LifecycleCapabilities {
	return LifecycleCapabilities{
		PublishAck: PublishAckBrokerPersisted, RequiredGroups: true, NoRequiredGroups: true,
		Retry: true, DeadLetter: true, SafeReclaim: true,
		RetainedMessages: true, RetainedBytes: true, Pending: true, Lag: true, OldestAge: true,
	}
}

func (n *NATSJetStreamProvider) EnsureLifecycle(ctx context.Context, policy LifecyclePolicy) error {
	if n == nil {
		return ErrNotConnected
	}
	policy = policy.Normalize()
	if err := policy.Validate(); err != nil {
		return err
	}
	n.mu.Lock()
	js := n.js
	n.mu.Unlock()
	if js == nil {
		return ErrNotConnected
	}

	stream, err := n.natsLifecycleStream(ctx, js, policy)
	if err != nil {
		return err
	}
	streamInfo, err := stream.Info(ctx)
	if err != nil {
		return err
	}
	if policy.NoRequiredGroups && streamInfo.State.Consumers != 0 {
		return fmt.Errorf("%w: NATS no-required-groups stream has consumers", ErrLifecycleStateUncertain)
	}
	store, err := n.natsLifecycleStore(ctx, js)
	if err != nil {
		return err
	}
	if err := n.ensureNATSLifecycleFingerprint(ctx, store, policy); err != nil {
		return err
	}
	for _, required := range policy.RequiredGroups {
		durable := n.natsLifecycleDurable(policy.Subject, required.Name)
		consumer, consumerErr := js.Consumer(ctx, streamInfo.Config.Name, durable)
		consumerExisted := consumerErr == nil
		if consumerErr != nil && !errors.Is(consumerErr, jetstream.ErrConsumerNotFound) {
			return consumerErr
		}
		enrollmentKey := n.natsLifecycleEnrollmentKey(policy.Subject, required.Name)
		if consumerExisted && required.Start == StartFromNew {
			if _, getErr := store.Get(ctx, enrollmentKey); getErr != nil {
				return fmt.Errorf("%w: existing NATS durable %q has unknown new-only enrollment", ErrLifecycleStateUncertain, durable)
			}
		}
		if !consumerExisted {
			config := n.natsLifecycleConsumerConfig(policy, required)
			consumer, err = stream.CreateOrUpdateConsumer(ctx, config)
			if err != nil {
				return fmt.Errorf("nats-jetstream: create lifecycle durable %q: %w", durable, err)
			}
		}
		consumerInfo, err := consumer.Info(ctx)
		if err != nil {
			return err
		}
		if consumerInfo.Config.AckPolicy != jetstream.AckExplicitPolicy {
			return fmt.Errorf("%w: NATS durable %q does not use explicit ACK", ErrLifecycleUnsafeBrokerPolicy, durable)
		}
		if err := validateNATSLifecycleConsumer(consumerInfo.Config, policy, required); err != nil {
			return err
		}
		enrollment := uint64(0)
		if required.Start == StartFromNew {
			enrollment = consumerInfo.Delivered.Stream
			if consumerExisted {
				// 重启读取首次 enrollment，不能用已经推进的 delivered 覆盖起点。
				enrollment, err = getNATSUint(ctx, store, enrollmentKey)
				if err != nil {
					return fmt.Errorf("%w: NATS enrollment missing", ErrLifecycleStateUncertain)
				}
			}
		}
		if err := createOrCompareNATSUint(ctx, store, enrollmentKey, enrollment); err != nil {
			return fmt.Errorf("%w: NATS durable %q enrollment conflict", ErrLifecyclePolicyConflict, durable)
		}
		completedKey := n.natsLifecycleCompletedKey(policy.Subject, required.Name)
		completed, completedErr := getNATSUint(ctx, store, completedKey)
		if errors.Is(completedErr, jetstream.ErrKeyNotFound) || errors.Is(completedErr, jetstream.ErrKeyDeleted) {
			if err := createOrCompareNATSUint(ctx, store, completedKey, enrollment); err != nil {
				return fmt.Errorf("%w: NATS durable %q completed frontier conflict", ErrLifecyclePolicyConflict, durable)
			}
		} else if completedErr != nil {
			return completedErr
		} else if completed < enrollment || completed > max(enrollment, consumerInfo.AckFloor.Stream) {
			return fmt.Errorf("%w: NATS durable %q completed frontier regressed", ErrLifecycleStateUncertain, durable)
		}
	}
	return nil
}

func (n *NATSJetStreamProvider) InspectLifecycle(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
	if n == nil {
		return LifecycleSnapshot{}, ErrNotConnected
	}
	policy = policy.Normalize()
	n.mu.Lock()
	js := n.js
	n.mu.Unlock()
	if js == nil {
		return LifecycleSnapshot{}, ErrNotConnected
	}
	store, err := n.natsLifecycleStore(ctx, js)
	if err != nil {
		return LifecycleSnapshot{}, err
	}
	if err := n.checkNATSLifecycleFingerprint(ctx, store, policy); err != nil {
		return LifecycleSnapshot{}, err
	}
	stream, err := js.Stream(ctx, natsResourceName(n.streamPrefix, policy.Subject))
	if err != nil {
		return LifecycleSnapshot{}, fmt.Errorf("%w: NATS lifecycle stream missing: %v", ErrLifecycleStateUncertain, err)
	}
	streamInfo, err := stream.Info(ctx)
	if err != nil {
		return LifecycleSnapshot{}, err
	}
	if err := validateNATSLifecycleStream(streamInfo.Config, policy, n.subjectKey(policy.Subject)); err != nil {
		return LifecycleSnapshot{}, err
	}

	safe := uint64(0)
	safeSet := false
	if policy.NoRequiredGroups {
		if streamInfo.State.Consumers != 0 {
			return LifecycleSnapshot{}, fmt.Errorf("%w: NATS no-required-groups stream has consumers", ErrLifecycleStateUncertain)
		}
		// 无消费完成承诺，候选范围仅限已观测末尾；实际删除仍逐条检查 Broker 时间。
		safe, safeSet = streamInfo.State.LastSeq, true
	}
	pendingTotal := int64(0)
	backlog := int64(0)
	groups := make([]ConsumerGroupSnapshot, 0, len(policy.RequiredGroups))
	for _, required := range policy.RequiredGroups {
		durable := n.natsLifecycleDurable(policy.Subject, required.Name)
		consumer, consumerErr := js.Consumer(ctx, streamInfo.Config.Name, durable)
		if consumerErr != nil {
			return LifecycleSnapshot{}, fmt.Errorf("%w: NATS required durable %q missing: %v", ErrLifecycleStateUncertain, durable, consumerErr)
		}
		info, infoErr := consumer.Info(ctx)
		if infoErr != nil {
			return LifecycleSnapshot{}, infoErr
		}
		if err := validateNATSLifecycleConsumer(info.Config, policy, required); err != nil {
			return LifecycleSnapshot{}, err
		}
		enrollment, enrollmentErr := getNATSUint(ctx, store, n.natsLifecycleEnrollmentKey(policy.Subject, required.Name))
		if enrollmentErr != nil {
			return LifecycleSnapshot{}, fmt.Errorf("%w: NATS durable %q enrollment missing", ErrLifecycleStateUncertain, durable)
		}
		frontier := info.AckFloor.Stream
		if frontier < enrollment {
			frontier = enrollment
		}
		completed, advanceErr := advanceNATSCompletedFrontier(ctx, store, n.natsLifecycleCompletedKey(policy.Subject, required.Name), frontier)
		if advanceErr != nil {
			return LifecycleSnapshot{}, fmt.Errorf("%w: NATS durable %q frontier regressed: %v", ErrLifecycleStateUncertain, durable, advanceErr)
		}
		if !safeSet || completed < safe {
			safe = completed
			safeSet = true
		}
		pending := int64(info.NumAckPending)
		lag := int64(info.NumPending)
		pendingTotal += pending
		if pending+lag > backlog {
			backlog = pending + lag
		}
		groups = append(groups, ConsumerGroupSnapshot{
			Name: required.Name, State: LifecycleStateOK,
			Pending: &pending, Lag: &lag,
			EnrollmentFrontier: strconv.FormatUint(enrollment, 10),
			CompletedFrontier:  strconv.FormatUint(completed, 10),
		})
	}
	retained := int64(streamInfo.State.Msgs)
	retainedBytes := int64(streamInfo.State.Bytes)
	var oldestAge *time.Duration
	if streamInfo.State.Msgs > 0 && !streamInfo.State.FirstTime.IsZero() {
		age := time.Since(streamInfo.State.FirstTime)
		if age < 0 {
			age = 0
		}
		oldestAge = &age
	}
	return LifecycleSnapshot{
		Subject: policy.Subject, PolicyFingerprint: policy.Fingerprint(),
		ObservedAt: time.Now(), State: LifecycleStateOK,
		RetainedMessages: &retained, RetainedBytes: &retainedBytes,
		BacklogMessages: &backlog, PendingMessages: &pendingTotal, OldestAge: oldestAge,
		SafeFrontier: strconv.FormatUint(safe, 10), Groups: groups,
	}, nil
}

func (n *NATSJetStreamProvider) ReclaimLifecycle(
	ctx context.Context,
	policy LifecyclePolicy,
	snapshot LifecycleSnapshot,
) (ReclaimResult, error) {
	started := time.Now()
	policy = policy.Normalize()
	if err := policy.Validate(); err != nil {
		return ReclaimResult{}, err
	}
	if policy.Mode != LifecycleModeEnforce {
		return ReclaimResult{Duration: time.Since(started)}, nil
	}
	if err := validateLifecycleSnapshot(policy, snapshot); err != nil {
		return ReclaimResult{}, err
	}
	n.mu.Lock()
	js := n.js
	n.mu.Unlock()
	if js == nil {
		return ReclaimResult{}, ErrNotConnected
	}
	store, err := n.natsLifecycleStore(ctx, js)
	if err != nil {
		return ReclaimResult{}, err
	}
	lease, coordinated := lifecycleLease(ctx, n, policy.Subject)
	ownerRevision := lease.revision
	fresh := snapshot
	if !coordinated {
		var acquired bool
		ownerRevision, acquired, err = acquireNATSReclaimLease(ctx, store, n.natsLifecycleLockKey(policy.Subject), policy.Reclaim.TimeBudget)
		if err != nil {
			return ReclaimResult{}, err
		}
		if !acquired {
			return ReclaimResult{Duration: time.Since(started)}, nil
		}
		defer func() {
			_, _ = store.Update(ctx, n.natsLifecycleLockKey(policy.Subject), []byte("released|0"), ownerRevision)
		}()
		// 公开直接调用仍重新采集，但必须先取得 owner。
		fresh, err = n.InspectLifecycle(ctx, policy)
		if err != nil {
			return ReclaimResult{}, err
		}
	}
	if err := checkNATSWorkerLease(ctx, store, n.natsLifecycleLockKey(policy.Subject), ownerRevision); err != nil {
		return ReclaimResult{}, err
	}
	safe, err := strconv.ParseUint(fresh.SafeFrontier, 10, 64)
	if err != nil || safe == 0 {
		return ReclaimResult{Duration: time.Since(started)}, nil
	}
	stream, err := js.Stream(ctx, natsResourceName(n.streamPrefix, policy.Subject))
	if err != nil {
		return ReclaimResult{}, err
	}
	before, err := stream.Info(ctx)
	if err != nil {
		return ReclaimResult{}, err
	}
	cutoff := time.Now().Add(-policy.Retention.MinAge)
	lastEligible := uint64(0)
	checked := 0
	for sequence := before.State.FirstSeq; sequence <= safe && checked < policy.Reclaim.BatchSize; sequence++ {
		if ctx.Err() != nil || time.Since(started) >= policy.Reclaim.TimeBudget {
			break
		}
		raw, getErr := stream.GetMsg(ctx, sequence)
		if errors.Is(getErr, jetstream.ErrMsgNotFound) {
			continue
		}
		if getErr != nil {
			return ReclaimResult{}, getErr
		}
		if raw.Time.After(cutoff) {
			break
		}
		lastEligible = sequence
		checked++
	}
	if lastEligible == 0 {
		return ReclaimResult{Duration: time.Since(started)}, nil
	}
	if policy.NoRequiredGroups {
		// JetStream 无 check-and-purge 事务；必须同时遵守管理面 ACL/维护窗口契约。
		if _, err := n.InspectLifecycle(ctx, policy); err != nil {
			return ReclaimResult{}, err
		}
	}
	if err := n.checkNATSLifecycleFingerprint(ctx, store, policy); err != nil {
		return ReclaimResult{}, err
	}
	if err := checkNATSWorkerLease(ctx, store, n.natsLifecycleLockKey(policy.Subject), ownerRevision); err != nil {
		return ReclaimResult{}, err
	}
	if err := stream.Purge(ctx, jetstream.WithPurgeSequence(lastEligible+1)); err != nil {
		return ReclaimResult{}, err
	}
	return ReclaimResult{
		Reclaimed: int64(checked), Duration: time.Since(started),
		BudgetExhausted: checked >= policy.Reclaim.BatchSize,
	}, nil
}

func (n *NATSJetStreamProvider) natsLifecycleStream(
	ctx context.Context,
	js jetstream.JetStream,
	policy LifecyclePolicy,
) (jetstream.Stream, error) {
	name := natsResourceName(n.streamPrefix, policy.Subject)
	stream, err := js.Stream(ctx, name)
	if err == nil {
		info, infoErr := stream.Info(ctx)
		if infoErr != nil {
			return nil, infoErr
		}
		if safetyErr := validateNATSLifecycleStream(info.Config, policy, n.subjectKey(policy.Subject)); safetyErr != nil {
			return nil, safetyErr
		}
		config := info.Config
		applyNATSLifecycleCapacity(&config, policy)
		updated, updateErr := js.CreateOrUpdateStream(ctx, config)
		return updated, updateErr
	}
	if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return nil, err
	}
	config := jetstream.StreamConfig{Name: name, Subjects: []string{n.subjectKey(policy.Subject)}}
	applyNATSLifecycleCapacity(&config, policy)
	created, createErr := js.CreateStream(ctx, config)
	return created, createErr
}

func applyNATSLifecycleCapacity(config *jetstream.StreamConfig, policy LifecyclePolicy) {
	if policy.Capacity.HardMessages > 0 {
		config.MaxMsgs = policy.Capacity.HardMessages
	}
	if policy.Capacity.HardBytes > 0 {
		config.MaxBytes = policy.Capacity.HardBytes
	}
	if policy.Capacity.HardMessages > 0 || policy.Capacity.HardBytes > 0 {
		config.Discard = jetstream.DiscardNew
	}
}

func validateNATSLifecycleStream(config jetstream.StreamConfig, policy LifecyclePolicy, expectedSubject string) error {
	if len(config.Subjects) != 1 || config.Subjects[0] != expectedSubject {
		return fmt.Errorf("%w: NATS lifecycle stream subject set is not exact", ErrLifecycleUnsafeBrokerPolicy)
	}
	if config.Retention != jetstream.LimitsPolicy || config.NoAck || config.Sealed || config.AllowMsgTTL ||
		config.MaxAge > 0 || config.MaxMsgsPerSubject > 0 || len(config.Sources) > 0 || config.Mirror != nil {
		return fmt.Errorf("%w: NATS stream has automatic or non-local retention", ErrLifecycleUnsafeBrokerPolicy)
	}
	if policy.Mode == LifecycleModeEnforce && config.DenyPurge {
		return fmt.Errorf("%w: NATS stream denies purge", ErrLifecycleUnsafeBrokerPolicy)
	}
	if config.MaxMsgs > 0 && (policy.Capacity.HardMessages != config.MaxMsgs || config.Discard != jetstream.DiscardNew) {
		return fmt.Errorf("%w: NATS MaxMsgs is not the declared fail-new capacity", ErrLifecycleUnsafeBrokerPolicy)
	}
	if config.MaxBytes > 0 && (policy.Capacity.HardBytes != config.MaxBytes || config.Discard != jetstream.DiscardNew) {
		return fmt.Errorf("%w: NATS MaxBytes is not the declared fail-new capacity", ErrLifecycleUnsafeBrokerPolicy)
	}
	return nil
}

func validateNATSLifecycleConsumer(
	config jetstream.ConsumerConfig,
	policy LifecyclePolicy,
	required ConsumerGroupRequirement,
) error {
	expectedDeliver := jetstream.DeliverAllPolicy
	if required.Start == StartFromNew {
		expectedDeliver = jetstream.DeliverNewPolicy
	}
	if config.Durable == "" || config.AckPolicy != jetstream.AckExplicitPolicy ||
		config.DeliverPolicy != expectedDeliver || config.MaxDeliver != -1 {
		return fmt.Errorf("%w: NATS durable %q has incompatible ACK, retry or start semantics", ErrLifecycleUnsafeBrokerPolicy, required.Name)
	}
	if policy.Retry.MaxAckPending > 0 && config.MaxAckPending != policy.Retry.MaxAckPending {
		return fmt.Errorf("%w: NATS durable %q MaxAckPending differs from lifecycle policy", ErrLifecycleUnsafeBrokerPolicy, required.Name)
	}
	return nil
}

func (n *NATSJetStreamProvider) natsLifecycleConsumerConfig(policy LifecyclePolicy, required ConsumerGroupRequirement) jetstream.ConsumerConfig {
	config := jetstream.ConsumerConfig{
		Durable:   n.natsLifecycleDurable(policy.Subject, required.Name),
		AckPolicy: jetstream.AckExplicitPolicy, DeliverPolicy: jetstream.DeliverAllPolicy,
		MaxDeliver: -1,
	}
	if required.Start == StartFromNew {
		config.DeliverPolicy = jetstream.DeliverNewPolicy
	}
	if policy.Retry.MaxAckPending > 0 {
		config.MaxAckPending = policy.Retry.MaxAckPending
	}
	return config
}

func (n *NATSJetStreamProvider) natsLifecycleDurable(subject, group string) string {
	return natsResourceName(n.durablePrefix, subject+"-"+group)
}

func (n *NATSJetStreamProvider) natsLifecycleStore(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	bucket := natsResourceName(n.streamPrefix, "lifecycle")
	return js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucket, History: 1, Storage: jetstream.FileStorage})
}

func (n *NATSJetStreamProvider) ensureNATSLifecycleFingerprint(ctx context.Context, store jetstream.KeyValue, policy LifecyclePolicy) error {
	key := n.natsLifecycleFingerprintKey(policy.Subject)
	value := []byte(natsLifecycleGeneration + ":" + policy.Fingerprint())
	if _, err := store.Create(ctx, key, value); err == nil {
		return nil
	} else if !errors.Is(err, jetstream.ErrKeyExists) {
		return err
	}
	entry, err := store.Get(ctx, key)
	if err != nil {
		return err
	}
	if string(entry.Value()) != string(value) {
		return fmt.Errorf("%w: NATS subject %q", ErrLifecyclePolicyConflict, policy.Subject)
	}
	return nil
}

func (n *NATSJetStreamProvider) checkNATSLifecycleFingerprint(ctx context.Context, store jetstream.KeyValue, policy LifecyclePolicy) error {
	entry, err := store.Get(ctx, n.natsLifecycleFingerprintKey(policy.Subject))
	if err != nil {
		return fmt.Errorf("%w: NATS lifecycle metadata missing: %w", ErrLifecycleStateUncertain, err)
	}
	expected := natsLifecycleGeneration + ":" + policy.Fingerprint()
	if string(entry.Value()) != expected {
		return fmt.Errorf("%w: NATS subject %q", ErrLifecyclePolicyConflict, policy.Subject)
	}
	return nil
}

func (n *NATSJetStreamProvider) natsLifecycleFingerprintKey(subject string) string {
	return "subject." + redisLifecycleSubjectHash(subject) + ".fingerprint"
}
func (n *NATSJetStreamProvider) natsLifecycleEnrollmentKey(subject, group string) string {
	return "subject." + redisLifecycleSubjectHash(subject) + ".group." + redisLifecycleGroupHash(group) + ".enrollment"
}
func (n *NATSJetStreamProvider) natsLifecycleCompletedKey(subject, group string) string {
	return "subject." + redisLifecycleSubjectHash(subject) + ".group." + redisLifecycleGroupHash(group) + ".completed"
}
func (n *NATSJetStreamProvider) natsLifecycleLockKey(subject string) string {
	return "subject." + redisLifecycleSubjectHash(subject) + ".reclaim-lock"
}

func createOrCompareNATSUint(ctx context.Context, store jetstream.KeyValue, key string, value uint64) error {
	encoded := []byte(strconv.FormatUint(value, 10))
	if _, err := store.Create(ctx, key, encoded); err == nil {
		return nil
	} else if !errors.Is(err, jetstream.ErrKeyExists) {
		return err
	}
	actual, err := store.Get(ctx, key)
	if err != nil {
		return err
	}
	if string(actual.Value()) != string(encoded) {
		return ErrLifecyclePolicyConflict
	}
	return nil
}

func getNATSUint(ctx context.Context, store jetstream.KeyValue, key string) (uint64, error) {
	entry, err := store.Get(ctx, key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(string(entry.Value()), 10, 64)
}

func advanceNATSCompletedFrontier(ctx context.Context, store jetstream.KeyValue, key string, candidate uint64) (uint64, error) {
	for {
		entry, err := store.Get(ctx, key)
		if err != nil {
			return 0, err
		}
		current, err := strconv.ParseUint(string(entry.Value()), 10, 64)
		if err != nil {
			return 0, err
		}
		if candidate < current {
			return 0, errors.New("completed frontier regressed")
		}
		if candidate == current {
			return current, nil
		}
		if _, err := store.Update(ctx, key, []byte(strconv.FormatUint(candidate, 10)), entry.Revision()); err == nil {
			return candidate, nil
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
	}
}

func acquireNATSReclaimLease(
	ctx context.Context,
	store jetstream.KeyValue,
	key string,
	timeBudget time.Duration,
) (uint64, bool, error) {
	ttl := 3 * timeBudget
	if ttl < time.Second {
		ttl = time.Second
	}
	for attempts := 0; attempts < 3; attempts++ {
		now := time.Now()
		value := []byte(fmt.Sprintf("%d|%d", natsLifecycleOwnerSequence.Add(1), now.Add(ttl).UnixNano()))
		entry, err := store.Get(ctx, key)
		if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
			revision, createErr := store.Create(ctx, key, value)
			if createErr == nil {
				return revision, true, nil
			}
			if !errors.Is(createErr, jetstream.ErrKeyExists) {
				return 0, false, createErr
			}
			continue
		}
		if err != nil {
			return 0, false, err
		}
		parts := strings.SplitN(string(entry.Value()), "|", 2)
		if len(parts) != 2 {
			return 0, false, ErrLifecycleStateUncertain
		}
		expiresAt, parseErr := strconv.ParseInt(parts[1], 10, 64)
		if parseErr != nil {
			return 0, false, ErrLifecycleStateUncertain
		}
		if expiresAt > now.UnixNano() {
			return 0, false, nil
		}
		revision, updateErr := store.Update(ctx, key, value, entry.Revision())
		if updateErr == nil {
			return revision, true, nil
		}
		if ctx.Err() != nil {
			return 0, false, ctx.Err()
		}
	}
	return 0, false, nil
}
