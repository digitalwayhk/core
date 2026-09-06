package mq

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

// LifecycleConformanceProvider 是统一生命周期行为测试所需的最小 Provider 组合。
type LifecycleConformanceProvider interface {
	LifecycleMQProvider
	ReliableMQProvider
}

// VerifyMessageLifecycleConformance 验证多组、失败 pending、离线组与完成后回收的共同契约。
// 调用方必须使用隔离的测试 Subject 和真实 Broker；本函数会发布并最终回收测试消息。
func VerifyMessageLifecycleConformance(
	ctx context.Context,
	provider LifecycleConformanceProvider,
	subject string,
) error {
	if provider == nil || subject == "" {
		return errors.New("mq lifecycle conformance: provider and subject are required")
	}
	policy := LifecyclePolicy{
		Subject: subject, Mode: LifecycleModeEnforce,
		RequiredGroups: []ConsumerGroupRequirement{
			{Name: "conformance-primary", Start: StartFromAllRetained},
			{Name: "conformance-offline", Start: StartFromAllRetained},
		},
		Reclaim: ReclaimBudget{Interval: time.Hour, BatchSize: 10, TimeBudget: time.Second},
	}.Normalize()
	if err := provider.EnsureLifecycle(ctx, policy); err != nil {
		return err
	}
	var allowPrimary atomic.Bool
	failed := make(chan struct{}, 1)
	primaryDone := make(chan struct{}, 1)
	primaryCancel, err := provider.SubscribeReliable(ctx, subject, ReliableSubscribeOptions{
		Group: "conformance-primary", Consumer: "conformance-primary-a", lifecycle: &policy,
	}, func(*Message) error {
		if !allowPrimary.Load() {
			select {
			case failed <- struct{}{}:
			default:
			}
			return errors.New("conformance failure")
		}
		select {
		case primaryDone <- struct{}{}:
		default:
		}
		return nil
	})
	if err != nil {
		return err
	}
	defer primaryCancel()
	if err := provider.Publish(ctx, subject, []byte("conformance-message"), nil); err != nil {
		return err
	}
	if err := waitLifecycleSignal(ctx, failed, "failed delivery"); err != nil {
		return err
	}
	if err := requireLifecycleReclaimBlocked(ctx, provider, policy, "failed pending"); err != nil {
		return err
	}

	allowPrimary.Store(true)
	if err := waitLifecycleSignal(ctx, primaryDone, "primary recovery"); err != nil {
		return err
	}
	if err := requireLifecycleReclaimBlocked(ctx, provider, policy, "offline required group"); err != nil {
		return err
	}

	offlineDone := make(chan struct{}, 1)
	offlineCancel, err := provider.SubscribeReliable(ctx, subject, ReliableSubscribeOptions{
		Group: "conformance-offline", Consumer: "conformance-offline-a", lifecycle: &policy,
	}, func(*Message) error {
		offlineDone <- struct{}{}
		return nil
	})
	if err != nil {
		return err
	}
	defer offlineCancel()
	if err := waitLifecycleSignal(ctx, offlineDone, "offline group recovery"); err != nil {
		return err
	}
	for {
		snapshot, inspectErr := provider.InspectLifecycle(ctx, policy)
		if inspectErr == nil && lifecycleFrontierAdvanced(snapshot.SafeFrontier) {
			result, reclaimErr := provider.ReclaimLifecycle(ctx, policy, snapshot)
			if reclaimErr != nil {
				return reclaimErr
			}
			if result.Reclaimed > 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("mq lifecycle conformance: completed message was not reclaimed: %w", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func requireLifecycleReclaimBlocked(
	ctx context.Context,
	provider LifecycleConformanceProvider,
	policy LifecyclePolicy,
	reason string,
) error {
	snapshot, err := provider.InspectLifecycle(ctx, policy)
	if err != nil {
		return err
	}
	result, err := provider.ReclaimLifecycle(ctx, policy, snapshot)
	if err != nil {
		return err
	}
	if result.Reclaimed != 0 {
		return fmt.Errorf("mq lifecycle conformance: reclaimed %d while %s", result.Reclaimed, reason)
	}
	return nil
}

func waitLifecycleSignal(ctx context.Context, signal <-chan struct{}, stage string) error {
	select {
	case <-signal:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("mq lifecycle conformance: timeout waiting for %s: %w", stage, ctx.Err())
	}
}

func lifecycleFrontierAdvanced(frontier string) bool {
	return frontier != "" && frontier != "0" && frontier != "0-0"
}
