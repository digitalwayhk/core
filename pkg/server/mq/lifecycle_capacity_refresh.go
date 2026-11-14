// 本文件在acquire、standby或owner观测超时后，用原轮次剩余预算补一次容量证据；不刷新回收前沿。
package mq

import (
	"context"
	"fmt"
	"time"
)

func (c *lifecycleController) refreshCapacityAfterTimeout(ctx context.Context, subject string, entry *lifecycleEntry, policy LifecyclePolicy, worker lifecycleWorkerProvider, cause error) {
	if lifecycleFailureReason(cause) != 0 || ctx.Err() != nil || c.ctx.Err() != nil {
		return
	}
	c.refreshLifecycleCapacity(ctx, subject, entry, policy, worker)
}

func (c *lifecycleController) refreshLifecycleCapacity(ctx context.Context, subject string, entry *lifecycleEntry, policy LifecyclePolicy, worker lifecycleWorkerProvider) {
	if policy.Capacity.HardMessages == 0 && policy.Capacity.HardBytes == 0 {
		return
	}
	c.mu.Lock()
	if c.entries[subject] != entry || entry.policy.Fingerprint() != policy.Fingerprint() {
		c.mu.Unlock()
		return
	}
	entry.diagnostics.refreshAttempts++
	c.mu.Unlock()
	// 不创建新超时/后台context；原runCtx最多只剩整轮尚未消耗的预算。
	capacity, err := worker.inspectLifecycleCapacity(ctx, policy)
	err = lifecyclePhaseError(err, ctx.Err())
	if err == nil && c.ctx.Err() != nil {
		err = c.ctx.Err()
	}
	if err == nil {
		err = validateLifecycleCapacity(policy, capacity, c.now())
	}
	c.recordLifecycleRead(subject, entry, "capacity", err)
	c.mu.Lock()
	defer c.mu.Unlock()
	// entry的provider在登记时冻结，指针和policy双核验防止迟到结果写入替换策略。
	if c.entries[subject] != entry || entry.policy.Fingerprint() != policy.Fingerprint() {
		return
	}
	// 返回和提交之间仍可能取消；持锁提交前再次核对，迟到成功不能刷新容量。
	if err == nil {
		err = lifecyclePhaseError(nil, ctx.Err())
		if err == nil {
			err = c.ctx.Err()
		}
		if err != nil {
			entry.diagnostics.capacityReadError = lifecycleReadErrorClass(err)
		}
	}
	if err != nil {
		entry.diagnostics.refreshFailures++
		// 补读失败不能重新采用被本轮失败遮蔽的旧容量；原完整快照与前沿仍保留。
		entry.capacity = &LifecycleSnapshot{Subject: subject, PolicyFingerprint: policy.Fingerprint(), State: LifecycleStateUnavailable}
		return
	}
	entry.capacity = &capacity
	entry.diagnostics.refreshSuccesses++
}

func validateLifecycleCapacity(policy LifecyclePolicy, snapshot LifecycleSnapshot, now time.Time) error {
	if snapshot.Subject != policy.Subject || snapshot.PolicyFingerprint != policy.Fingerprint() {
		return fmt.Errorf("%w: capacity identity", ErrLifecyclePolicyConflict)
	}
	if snapshot.ObservedAt.IsZero() || now.Sub(snapshot.ObservedAt) > 2*policy.Reclaim.Interval || (snapshot.State != LifecycleStateOK && snapshot.State != LifecycleStatePartial) {
		return fmt.Errorf("%w: capacity evidence", ErrLifecycleStateUncertain)
	}
	if policy.Capacity.HardMessages > 0 && (snapshot.RetainedMessages == nil || *snapshot.RetainedMessages < 0) {
		return fmt.Errorf("%w: capacity messages", ErrLifecycleStateUncertain)
	}
	if policy.Capacity.HardBytes > 0 && (snapshot.RetainedBytes == nil || *snapshot.RetainedBytes < 0) {
		return fmt.Errorf("%w: capacity bytes", ErrLifecycleStateUncertain)
	}
	return nil
}
