// 本文件提供 JetStream 生命周期扫描前的兼容 KV 租约和轻量容量读取。
package mq

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func (n *NATSJetStreamProvider) releaseLifecycleRound(ctx context.Context, policy LifecyclePolicy, owner string) error {
	n.mu.Lock()
	js := n.js
	n.mu.Unlock()
	if js == nil {
		return ErrNotConnected
	}
	store, err := n.natsLifecycleStore(ctx, js)
	if err != nil {
		return err
	}
	key := n.natsLifecycleLockKey(policy.Subject)
	entry, err := store.Get(ctx, key)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(entry.Value()), owner+"|") {
		return nil
	}
	_, err = store.Update(ctx, key, []byte("released|0"), entry.Revision())
	return err
}

func (n *NATSJetStreamProvider) acquireLifecycleRound(ctx context.Context, policy LifecyclePolicy, owner string) (context.Context, bool, error) {
	n.mu.Lock()
	js := n.js
	n.mu.Unlock()
	if js == nil {
		return ctx, false, ErrNotConnected
	}
	store, err := n.natsLifecycleStore(ctx, js)
	if err != nil {
		return ctx, false, err
	}
	key := n.natsLifecycleLockKey(policy.Subject)
	entry, err := store.Get(ctx, key)
	missing := errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted)
	if err != nil && !missing {
		return ctx, false, err
	}
	if !missing {
		parts := strings.SplitN(string(entry.Value()), "|", 2)
		if len(parts) != 2 {
			return ctx, false, ErrLifecycleStateUncertain
		}
		expiry, parseErr := strconv.ParseInt(parts[1], 10, 64)
		if parseErr != nil {
			return ctx, false, ErrLifecycleStateUncertain
		}
		if parts[0] != owner && expiry > time.Now().UnixNano() {
			return ctx, false, nil
		}
	}
	value := []byte(owner + "|" + strconv.FormatInt(time.Now().Add(lifecycleWorkerLeaseTTL(policy)).UnixNano(), 10))
	var revision uint64
	if missing {
		revision, err = store.Create(ctx, key, value)
	} else {
		revision, err = store.Update(ctx, key, value, entry.Revision())
	}
	if errors.Is(err, jetstream.ErrKeyExists) {
		return ctx, false, nil
	}
	if err != nil {
		return ctx, false, err
	}
	lease := lifecycleRoundLease{provider: n, subject: policy.Subject, owner: owner, revision: revision}
	return context.WithValue(ctx, lifecycleRoundKey{}, lease), true, nil
}

func (n *NATSJetStreamProvider) inspectLifecycleCapacity(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
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
		return LifecycleSnapshot{}, err
	}
	info := stream.CachedInfo()
	if err := validateNATSLifecycleStream(info.Config, policy, n.subjectKey(policy.Subject)); err != nil {
		return LifecycleSnapshot{}, err
	}
	count, bytes := int64(info.State.Msgs), int64(info.State.Bytes)
	return LifecycleSnapshot{Subject: policy.Subject, PolicyFingerprint: policy.Fingerprint(), ObservedAt: time.Now(), State: LifecycleStatePartial, RetainedMessages: &count, RetainedBytes: &bytes}, nil
}

func checkNATSWorkerLease(ctx context.Context, store jetstream.KeyValue, key string, revision uint64) error {
	entry, err := store.Get(ctx, key)
	if err != nil {
		return err
	}
	parts := strings.SplitN(string(entry.Value()), "|", 2)
	if entry.Revision() != revision || len(parts) != 2 {
		return errLifecycleOwnerLost
	}
	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || expiry <= time.Now().UnixNano() {
		return errLifecycleOwnerLost
	}
	return nil
}
