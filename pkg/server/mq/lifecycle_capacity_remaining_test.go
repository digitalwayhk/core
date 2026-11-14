// 本文件在正式controller和真实Redis上复现acquire/standby未使用原轮次余量的容量缺口。
package mq

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type remainingCapacityRedis struct {
	*RedisStreamProvider
	stage            string
	light            atomic.Int64
	terminal         error
	exhausted        bool
	afterLight       func()
	inspectDeadline  time.Time
	fallbackDeadline time.Time
}

func (p *remainingCapacityRedis) acquireLifecycleRound(ctx context.Context, policy LifecyclePolicy, owner string) (context.Context, bool, error) {
	p.inspectDeadline, _ = ctx.Deadline()
	if p.stage == "acquire" {
		<-ctx.Done()
		if p.terminal != nil {
			return ctx, false, p.terminal
		}
		return ctx, false, ctx.Err()
	}
	return p.RedisStreamProvider.acquireLifecycleRound(ctx, policy, owner)
}
func (p *remainingCapacityRedis) inspectLifecycleCapacity(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
	if p.light.Add(1) == 1 && p.stage == "standby" {
		<-ctx.Done()
		if p.terminal != nil {
			return LifecycleSnapshot{}, p.terminal
		}
		return LifecycleSnapshot{}, ctx.Err()
	}
	p.fallbackDeadline, _ = ctx.Deadline()
	if p.exhausted {
		<-ctx.Done()
		return LifecycleSnapshot{}, ctx.Err()
	}
	snapshot, err := p.RedisStreamProvider.inspectLifecycleCapacity(ctx, policy)
	if p.afterLight != nil {
		p.afterLight()
	}
	return snapshot, err
}

// TestRedisCapacityRemainingBudget 固定350ms子预算到期、真实容量仍可用，原500ms余量不得丢失。
func TestRedisCapacityRemainingBudget(t *testing.T) {
	for _, stage := range []string{"acquire", "standby"} {
		t.Run(stage, func(t *testing.T) {
			r := newWindowRedisProvider(t)
			ctx := context.Background()
			policy := validLifecyclePolicy("required")
			policy.Subject = "remaining-" + stage
			policy.Reclaim.Interval = 2 * time.Second
			policy.Reclaim.TimeBudget = 500 * time.Millisecond
			policy.Capacity.HardBytes = 256 << 20
			require.NoError(t, r.EnsureLifecycle(ctx, policy))
			before, err := r.InspectLifecycle(ctx, policy)
			require.NoError(t, err)
			before.ObservedAt = time.Now().Add(-5 * time.Second)
			require.NoError(t, r.client.Set(ctx, r.lifecycleOwnerKey(policy.Subject), "other-owner", time.Minute).Err())
			p := &remainingCapacityRedis{RedisStreamProvider: r, stage: stage}
			c := newLifecycleController()
			defer c.stop()
			c.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: p, snapshot: before}
			start := time.Now()
			roundErr := c.runOnce(ctx, policy.Subject)
			elapsed := time.Since(start)
			require.ErrorIs(t, roundErr, context.DeadlineExceeded)
			require.Equal(t, before, c.entries[policy.Subject].snapshot)
			manager := &MQManager{current: r, lifecycle: c}
			publishErr := manager.Publish(ctx, policy.Subject, []byte("Core合成容量原件"), nil)
			t.Logf("stage=%s round=%v elapsed=%s capacity_calls=%d publish=%v", stage, roundErr, elapsed, p.light.Load(), publishErr)
			require.NoError(t, publishErr, "CAPACITY_REMAINING_BUDGET_GAP: 原轮次有余量且真实容量可读，不能仅因acquire/standby超时保持旧证据")
			require.Equal(t, policy.Fingerprint(), r.client.HGet(ctx, r.lifecycleMetaKey(policy.Subject), "fingerprint").Val())
			require.True(t, p.fallbackDeadline.After(p.inspectDeadline))
			require.LessOrEqual(t, p.fallbackDeadline.Sub(p.inspectDeadline), policy.Reclaim.TimeBudget*3/10, fmt.Sprint("实际截止时间只能继承原轮次余量，不能另开预算"))
		})
	}
}

// TestRedisCapacityRemainingSafety 验证两新增入口的语义错误、原预算耗尽和迟到取消仍失败关闭。
func TestRedisCapacityRemainingSafety(t *testing.T) {
	for _, stage := range []string{"acquire", "standby"} {
		for _, mode := range []string{"policy", "owner", "cancel", "exhausted", "late"} {
			t.Run(stage+"/"+mode, func(t *testing.T) {
				r := newWindowRedisProvider(t)
				ctx := context.Background()
				policy := validLifecyclePolicy("g")
				policy.Subject = "remaining-safety"
				policy.Capacity.HardMessages = 10
				policy.Reclaim.TimeBudget = 100 * time.Millisecond
				require.NoError(t, r.EnsureLifecycle(ctx, policy))
				old, err := r.InspectLifecycle(ctx, policy)
				require.NoError(t, err)
				old.ObservedAt = time.Now().Add(-time.Hour)
				require.NoError(t, r.client.Set(ctx, r.lifecycleOwnerKey(policy.Subject), "other", time.Minute).Err())
				p := &remainingCapacityRedis{RedisStreamProvider: r, stage: stage}
				c := newLifecycleController()
				defer c.stop()
				entry := &lifecycleEntry{policy: policy, provider: p, snapshot: old}
				c.entries[policy.Subject] = entry
				switch mode {
				case "policy":
					p.terminal = errors.Join(ErrLifecyclePolicyConflict, context.DeadlineExceeded)
				case "owner":
					p.terminal = errors.Join(errLifecycleOwnerLost, context.DeadlineExceeded)
				case "cancel":
					p.terminal = context.Canceled
				case "exhausted":
					p.exhausted = true
				case "late":
					p.afterLight = c.cancel
				}
				roundErr := c.runOnce(ctx, policy.Subject)
				require.Error(t, roundErr)
				require.Equal(t, old, entry.snapshot)
				require.ErrorIs(t, c.allowPublish(policy.Subject), ErrLifecycleCapacityUnknown)
				expected := int64(0)
				if stage == "standby" {
					expected = 1
				}
				if mode == "exhausted" || mode == "late" {
					expected++
				}
				require.Equal(t, expected, p.light.Load())
				require.Zero(t, entry.diagnostics.refreshSuccesses)
				if mode == "policy" {
					require.ErrorIs(t, roundErr, ErrLifecyclePolicyConflict)
				}
			})
		}
	}
}
