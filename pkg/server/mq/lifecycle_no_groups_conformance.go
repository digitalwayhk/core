package mq

import (
	"context"
	"fmt"
	"time"
)

// verifyNoRequiredGroupsLifecycleConformance 在隔离主题验证观察、背压、迁移冲突和到期有界回收。
func verifyNoRequiredGroupsLifecycleConformance(ctx context.Context, provider LifecycleMQProvider, subject string) error {
	policy := LifecyclePolicy{
		Subject: subject, NoRequiredGroups: true, Mode: LifecycleModeObserve,
		Retention: RetentionPolicy{MinAge: 200 * time.Millisecond},
		Capacity:  CapacityPolicy{HardMessages: 3},
		Reclaim:   ReclaimBudget{Interval: time.Hour, BatchSize: 2, TimeBudget: time.Second},
	}.Normalize()
	manager := NewManager()
	manager.Register(provider)
	if err := manager.SetCurrent(provider.Name()); err != nil {
		return err
	}
	// Provider 归外层 conformance 调用方所有，此处只停止临时控制器。
	defer manager.lifecycle.stop()
	if err := manager.RequireMessageLifecycle(ctx, policy); err != nil {
		return err
	}
	for i := 0; i < 3; i++ {
		if err := manager.Publish(ctx, subject, []byte("retained-history"), nil); err != nil {
			return err
		}
	}
	if err := manager.lifecycle.runOnce(ctx, subject); err != nil {
		return err
	}
	if err := manager.Publish(ctx, subject, []byte("capacity-rejected"), nil); err != ErrLifecycleBackpressure {
		return fmt.Errorf("mq no-group conformance: capacity must reject: %v", err)
	}
	snapshot, err := provider.InspectLifecycle(ctx, policy)
	if err != nil {
		return err
	}
	if snapshot.RetainedMessages == nil || *snapshot.RetainedMessages != 3 || len(snapshot.Groups) != 0 {
		return fmt.Errorf("mq no-group conformance: invalid retained snapshot")
	}
	if err := requireNoGroupReclaimed(ctx, provider, policy, snapshot, 0); err != nil {
		return err
	}
	changed := policy
	changed.NoRequiredGroups = false
	changed.RequiredGroups = []ConsumerGroupRequirement{{Name: "new-reader", Start: StartFromAllRetained}}
	if err := provider.EnsureLifecycle(ctx, changed); err == nil {
		return fmt.Errorf("mq no-group conformance: group migration must conflict")
	}
	timer := time.NewTimer(policy.Retention.MinAge + 20*time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	// 观察模式即使已经到期，也不能直接通过 Provider 删除。
	if err := requireNoGroupReclaimed(ctx, provider, policy, snapshot, 0); err != nil {
		return err
	}
	policy.Mode = LifecycleModeEnforce
	if err := provider.EnsureLifecycle(ctx, policy); err != nil {
		return err
	}
	snapshot, err = provider.InspectLifecycle(ctx, policy)
	if err != nil {
		return err
	}
	if err := requireNoGroupReclaimed(ctx, provider, policy, snapshot, 2); err != nil {
		return err
	}
	snapshot, err = provider.InspectLifecycle(ctx, policy)
	if err != nil {
		return err
	}
	if err := requireNoGroupReclaimed(ctx, provider, policy, snapshot, 1); err != nil {
		return err
	}
	snapshot, err = provider.InspectLifecycle(ctx, policy)
	if err != nil {
		return err
	}
	if snapshot.RetainedMessages == nil || *snapshot.RetainedMessages != 0 {
		return fmt.Errorf("mq no-group conformance: retained history not reclaimed")
	}
	return nil
}

func requireNoGroupReclaimed(ctx context.Context, provider LifecycleMQProvider, policy LifecyclePolicy, snapshot LifecycleSnapshot, expected int64) error {
	result, err := provider.ReclaimLifecycle(ctx, policy, snapshot)
	if err != nil {
		return err
	}
	if result.Reclaimed != expected {
		return fmt.Errorf("mq no-group conformance: reclaimed %d, expected %d", result.Reclaimed, expected)
	}
	return nil
}
