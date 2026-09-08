package mq

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

type lifecycleEntry struct {
	policy   LifecyclePolicy
	provider LifecycleMQProvider
	snapshot LifecycleSnapshot
	capacity *LifecycleSnapshot
	running  atomic.Bool
}

// lifecycleController 持有已冻结的 Subject 策略、快照和有界回收 worker。
type lifecycleController struct {
	mu              sync.RWMutex
	entries         map[string]*lifecycleEntry
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	now             func() time.Time
	close           sync.Once
	owner           string
	reclaimed       atomic.Int64
	reclaimFailed   atomic.Int64
	reclaimReasons  [4]atomic.Int64
	publishRejected atomic.Int64
}

func newLifecycleController() *lifecycleController {
	ctx, cancel := context.WithCancel(context.Background())
	return &lifecycleController{
		entries: make(map[string]*lifecycleEntry),
		owner:   fmt.Sprintf("%016x%016x", rand.Uint64(), rand.Uint64()),
		ctx:     ctx,
		cancel:  cancel,
		now:     time.Now,
	}
}

func (c *lifecycleController) require(
	ctx context.Context,
	provider LifecycleMQProvider,
	policy LifecyclePolicy,
) error {
	if c == nil || provider == nil {
		return ErrLifecycleUnsupported
	}
	policy = policy.Normalize()
	if err := policy.Validate(); err != nil {
		return err
	}
	if err := validateLifecycleCapabilities(provider.LifecycleCapabilities(), policy); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil {
		return ErrNotConnected
	}
	if existing := c.entries[policy.Subject]; existing != nil {
		if existing.policy.Fingerprint() != policy.Fingerprint() {
			return fmt.Errorf("%w: subject %q", ErrLifecyclePolicyConflict, policy.Subject)
		}
		return nil
	}
	if err := provider.EnsureLifecycle(ctx, policy); err != nil {
		return fmt.Errorf("mq: ensure lifecycle for %q: %w", policy.Subject, err)
	}
	snapshot, err := provider.InspectLifecycle(ctx, policy)
	if err != nil {
		return fmt.Errorf("mq: inspect lifecycle for %q: %w", policy.Subject, err)
	}
	if err := validateLifecycleSnapshot(policy, snapshot); err != nil {
		return err
	}
	c.entries[policy.Subject] = &lifecycleEntry{
		policy: policy, provider: provider, snapshot: snapshot,
	}
	c.wg.Add(1)
	go c.runWorker(policy.Subject, policy.Reclaim.Interval)
	return nil
}

func validateLifecycleCapabilities(capability LifecycleCapabilities, policy LifecyclePolicy) error {
	if !publishAckSatisfies(capability.PublishAck, policy.RequiredPublishAck) ||
		(policy.NoRequiredGroups && !capability.NoRequiredGroups) ||
		(!policy.NoRequiredGroups && !capability.RequiredGroups) {
		return ErrLifecycleUnsupported
	}
	if policy.Mode == LifecycleModeEnforce && !capability.SafeReclaim {
		return ErrLifecycleUnsupported
	}
	if policy.Retry.MaxDeliveries > 0 && (!capability.Retry || !capability.DeadLetter) {
		return ErrLifecycleUnsupported
	}
	if (policy.Capacity.SoftMessages > 0 || policy.Capacity.HardMessages > 0) && !capability.RetainedMessages {
		return ErrLifecycleUnsupported
	}
	if (policy.Capacity.SoftBytes > 0 || policy.Capacity.HardBytes > 0) && !capability.RetainedBytes {
		return ErrLifecycleUnsupported
	}
	return nil
}

func publishAckSatisfies(actual, required PublishAckLevel) bool {
	if required == PublishAckBrokerAccepted {
		return actual == PublishAckBrokerAccepted || actual == PublishAckBrokerPersisted
	}
	return actual == PublishAckBrokerPersisted
}

func validateLifecycleSnapshot(policy LifecyclePolicy, snapshot LifecycleSnapshot) error {
	if snapshot.Subject != "" && snapshot.Subject != policy.Subject {
		return fmt.Errorf("%w: snapshot subject %q does not match %q", ErrLifecycleStateUncertain, snapshot.Subject, policy.Subject)
	}
	if snapshot.PolicyFingerprint != "" && snapshot.PolicyFingerprint != policy.Fingerprint() {
		return fmt.Errorf("%w: snapshot fingerprint does not match subject %q", ErrLifecyclePolicyConflict, policy.Subject)
	}
	if snapshot.State == LifecycleStateUnavailable || snapshot.State == LifecycleStateStale {
		return fmt.Errorf("%w: subject %q snapshot state %q", ErrLifecycleStateUncertain, policy.Subject, snapshot.State)
	}
	return nil
}

func (c *lifecycleController) runWorker(subject string, interval time.Duration) {
	defer c.wg.Done()
	timer := time.NewTimer(lifecycleWorkerDelay(interval, true))
	defer timer.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-timer.C:
			_ = c.runOnce(c.ctx, subject)
			timer.Reset(lifecycleWorkerDelay(interval, false))
		}
	}
}

func (c *lifecycleController) runOnce(ctx context.Context, subject string) (runErr error) {
	c.mu.RLock()
	entry := c.entries[subject]
	if entry == nil {
		c.mu.RUnlock()
		return nil
	}
	policy := entry.policy
	provider := entry.provider
	c.mu.RUnlock()
	if !entry.running.CompareAndSwap(false, true) {
		return nil
	}
	defer entry.running.Store(false)
	defer func() {
		if runErr != nil && policy.Mode == LifecycleModeEnforce {
			c.reclaimFailed.Add(1)
			c.reclaimReasons[lifecycleFailureReason(runErr)].Add(1)
		}
	}()

	runCtx, cancel := context.WithTimeout(ctx, policy.Reclaim.TimeBudget)
	defer cancel()
	// 整轮预算不变：观测最多使用 70%，剩余时间留给独立的回收阶段。
	inspectCtx, stopInspect := context.WithTimeout(runCtx, policy.Reclaim.TimeBudget*7/10)
	defer stopInspect()
	if worker, ok := provider.(lifecycleWorkerProvider); ok {
		ownedCtx, owned, err := worker.acquireLifecycleRound(inspectCtx, policy, c.owner)
		if err != nil {
			if inspectCtx.Err() != nil {
				return inspectCtx.Err()
			}
			return err
		}
		if !owned {
			capacity, err := worker.inspectLifecycleCapacity(inspectCtx, policy)
			if err != nil {
				if inspectCtx.Err() != nil {
					return inspectCtx.Err()
				}
				return err
			}
			c.mu.Lock()
			entry.capacity = &capacity
			c.mu.Unlock()
			return nil
		}
		// 仅复制租约凭据，不继承观测子 context 的取消信号。
		runCtx = context.WithValue(runCtx, lifecycleRoundKey{}, ownedCtx.Value(lifecycleRoundKey{}))
		inspectCtx = ownedCtx
	}
	snapshot, err := provider.InspectLifecycle(inspectCtx, policy)
	if phaseErr := inspectCtx.Err(); phaseErr != nil {
		err = phaseErr
	}
	stopInspect()
	if err != nil {
		return err
	}
	if err := validateLifecycleSnapshot(policy, snapshot); err != nil {
		return err
	}
	c.mu.Lock()
	current := c.entries[subject]
	if current == nil || current.policy.Fingerprint() != policy.Fingerprint() {
		c.mu.Unlock()
		return ErrLifecyclePolicyConflict
	}
	current.snapshot = snapshot
	current.capacity = nil
	c.mu.Unlock()

	if policy.Mode != LifecycleModeEnforce {
		return nil
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	result, err := provider.ReclaimLifecycle(runCtx, policy, snapshot)
	if err != nil {
		if runCtx.Err() != nil {
			return runCtx.Err()
		}
		return err
	}
	c.reclaimed.Add(result.Reclaimed)
	return err
}

func (c *lifecycleController) recordPublishRejected(subject string) {
	if c == nil {
		return
	}
	c.mu.RLock()
	_, declared := c.entries[subject]
	c.mu.RUnlock()
	if declared {
		c.publishRejected.Add(1)
	}
}

func (c *lifecycleController) allowPublish(subject string) error {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	entry := c.entries[subject]
	if entry == nil {
		c.mu.RUnlock()
		return nil
	}
	policy := entry.policy
	snapshot := entry.snapshot
	if entry.capacity != nil {
		snapshot = *entry.capacity
	}
	c.mu.RUnlock()
	if policy.Capacity.HardMessages == 0 && policy.Capacity.HardBytes == 0 {
		return nil
	}
	if snapshot.ObservedAt.IsZero() || c.now().Sub(snapshot.ObservedAt) > 2*policy.Reclaim.Interval ||
		(snapshot.State != LifecycleStateOK && snapshot.State != LifecycleStatePartial) {
		return ErrLifecycleCapacityUnknown
	}
	if policy.Capacity.HardMessages > 0 {
		if snapshot.RetainedMessages == nil {
			return ErrLifecycleCapacityUnknown
		}
		if *snapshot.RetainedMessages >= policy.Capacity.HardMessages {
			return ErrLifecycleBackpressure
		}
	}
	if policy.Capacity.HardBytes > 0 {
		if snapshot.RetainedBytes == nil {
			return ErrLifecycleCapacityUnknown
		}
		if *snapshot.RetainedBytes >= policy.Capacity.HardBytes {
			return ErrLifecycleBackpressure
		}
	}
	return nil
}

func (c *lifecycleController) validateRequiredGroup(subject, group string) error {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	entry := c.entries[subject]
	c.mu.RUnlock()
	if entry == nil {
		return nil
	}
	for _, required := range entry.policy.RequiredGroups {
		if required.Name == group {
			return nil
		}
	}
	return fmt.Errorf("%w: subject %q group %q", ErrLifecycleRequiredGroupMismatch, subject, group)
}

func (c *lifecycleController) policyForSubject(subject string) *LifecyclePolicy {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	entry := c.entries[subject]
	c.mu.RUnlock()
	if entry == nil {
		return nil
	}
	policy := entry.policy.Normalize()
	return &policy
}

func (c *lifecycleController) stop() {
	if c == nil {
		return
	}
	c.close.Do(func() {
		c.cancel()
		c.wg.Wait()
		// 全部租约清理共享一个关闭预算；失败时由有限 TTL 接管，不逐 subject 无限等待。
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c.mu.RLock()
		defer c.mu.RUnlock()
		for _, entry := range c.entries {
			if ctx.Err() != nil {
				break
			}
			if releaser, ok := entry.provider.(lifecycleWorkerReleaser); ok {
				_ = releaser.releaseLifecycleRound(ctx, entry.policy, c.owner)
			}
		}
	})
}
