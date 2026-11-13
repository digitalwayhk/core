// 本文件复用Core正式控制器与真实Redis，回归owner检查超时后的容量刷新隔离。
package mq

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// capacityRefreshDiagnosticProvider仅扩展Core原有worker夹具，两个角色使用同一个合法容量源。
type capacityRefreshDiagnosticProvider struct {
	workerTestProvider
	capacityCalls atomic.Int64
	fullCalls     atomic.Int64
	full          func(context.Context, LifecyclePolicy) (LifecycleSnapshot, error)
	light         func(context.Context, LifecyclePolicy) (LifecycleSnapshot, error)
}

func (p *capacityRefreshDiagnosticProvider) inspectLifecycleCapacity(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
	p.capacityCalls.Add(1)
	if p.light != nil {
		return p.light(ctx, policy)
	}
	if err := ctx.Err(); err != nil {
		return LifecycleSnapshot{}, err
	}
	count, bytes := int64(0), int64(4096)
	return LifecycleSnapshot{Subject: policy.Subject, PolicyFingerprint: policy.Fingerprint(), ObservedAt: time.Now(), State: LifecycleStatePartial, RetainedMessages: &count, RetainedBytes: &bytes}, nil
}

// InspectLifecycle在边界用例注入完整错误；默认仍执行Core原有worker夹具。
func (p *capacityRefreshDiagnosticProvider) InspectLifecycle(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
	p.fullCalls.Add(1)
	if p.full != nil {
		return p.full(ctx, policy)
	}
	return p.workerTestProvider.InspectLifecycle(ctx, policy)
}

// TestCapacityRefreshDiagnosticController用正式controller和现有worker夹具验证owner完整检查超时，非owner轻量容量仍可用；旧代码应真实RED。
func TestCapacityRefreshDiagnosticController(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprintf("owner=%t", owned), func(t *testing.T) {
			policy := validLifecyclePolicy("required")
			policy.Subject = "core-capacity-diagnostic"
			policy.Reclaim.Interval = 2 * time.Second
			policy.Reclaim.TimeBudget = 500 * time.Millisecond
			policy.Capacity.HardBytes = 256 << 20
			p := &capacityRefreshDiagnosticProvider{workerTestProvider: workerTestProvider{owner: owned, delay: 400 * time.Millisecond}}
			c := newLifecycleController()
			defer c.stop()
			count, bytes := int64(0), int64(4096)
			c.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: p, snapshot: LifecycleSnapshot{Subject: policy.Subject, PolicyFingerprint: policy.Fingerprint(), ObservedAt: time.Now().Add(-5 * time.Second), State: LifecycleStateOK, RetainedMessages: &count, RetainedBytes: &bytes}}
			started := time.Now()
			roundErr := c.runOnce(context.Background(), policy.Subject)
			publishErr := c.allowPublish(policy.Subject)
			if owned {
				require.ErrorIs(t, roundErr, context.DeadlineExceeded)
				require.EqualValues(t, 1, c.reclaimReasons[0].Load())
			} else {
				require.NoError(t, roundErr)
			}
			require.Zero(t, p.reclaimCalls)
			c.mu.RLock()
			fullAt := c.entries[policy.Subject].snapshot.ObservedAt
			capacityPresent := c.entries[policy.Subject].capacity != nil
			c.mu.RUnlock()
			t.Logf("owner=%t budget=500ms inspect=350ms elapsed=%s round=%v capacity_calls=%d capacity_present=%t full_age=%s publish=%v", owned, time.Since(started), roundErr, p.capacityCalls.Load(), capacityPresent, time.Since(fullAt), publishErr)
			require.NoError(t, publishErr, "CAPACITY_REFRESH_OWNER_GAP: 同样新鲜且低于硬限的容量可用，不应仅因owner全组检查超时丧失容量刷新")
		})
	}
}

type capacityRefreshDiagnosticRedis struct {
	*RedisStreamProvider
	fullCalls         atomic.Int64
	capacityCalls     atomic.Int64
	reclaimCalls      atomic.Int64
	ownerLightCalls   atomic.Int64
	standbyLightCalls atomic.Int64
	maxOwnerRemaining atomic.Int64
	afterFullTimeout  func()
}

func (p *capacityRefreshDiagnosticRedis) InspectLifecycle(ctx context.Context, _ LifecyclePolicy) (LifecycleSnapshot, error) {
	p.fullCalls.Add(1)
	<-ctx.Done()
	if p.afterFullTimeout != nil {
		p.afterFullTimeout()
	}
	return LifecycleSnapshot{}, ctx.Err()
}
func (p *capacityRefreshDiagnosticRedis) inspectLifecycleCapacity(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
	p.capacityCalls.Add(1)
	if _, owned := lifecycleLease(ctx, p.RedisStreamProvider, policy.Subject); owned {
		p.ownerLightCalls.Add(1)
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline).Nanoseconds()
			for {
				old := p.maxOwnerRemaining.Load()
				if remaining <= old || p.maxOwnerRemaining.CompareAndSwap(old, remaining) {
					break
				}
			}
		}
	} else {
		p.standbyLightCalls.Add(1)
	}
	return p.RedisStreamProvider.inspectLifecycleCapacity(ctx, policy)
}
func (p *capacityRefreshDiagnosticRedis) ReclaimLifecycle(ctx context.Context, policy LifecyclePolicy, snapshot LifecycleSnapshot) (ReclaimResult, error) {
	p.reclaimCalls.Add(1)
	return p.RedisStreamProvider.ReclaimLifecycle(ctx, policy, snapshot)
}

// TestCapacityRefreshDiagnosticRedisOwnerStandby真实Redis lease/轻量管线/实际Publish，完整Inspect仅注入可控deadline，不宣称Redis自然延迟复现。
func TestCapacityRefreshDiagnosticRedisOwnerStandby(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: 需要本轮独占Redis")
	}
	ctx := context.Background()
	prefix := fmt.Sprintf("core:capacity-diagnostic:%d", time.Now().UnixNano())
	r := NewRedisStreamProvider(addr, prefix, 0)
	require.NoError(t, r.Connect(ctx))
	defer r.Close()
	policy := validLifecyclePolicy("required")
	policy.Subject = "core-capacity-diagnostic"
	policy.Reclaim.Interval = 2 * time.Second
	policy.Reclaim.TimeBudget = 500 * time.Millisecond
	policy.Capacity.SoftBytes = 192 << 20
	policy.Capacity.HardBytes = 256 << 20
	require.NoError(t, r.EnsureLifecycle(ctx, policy))
	before, err := r.InspectLifecycle(ctx, policy)
	require.NoError(t, err)
	require.NotNil(t, before.RetainedBytes)
	require.Less(t, *before.RetainedBytes, policy.Capacity.HardBytes)
	before.ObservedAt = time.Now().Add(-5 * time.Second)
	p := &capacityRefreshDiagnosticRedis{RedisStreamProvider: r}
	owner := newLifecycleController()
	defer owner.stop()
	owner.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: p, snapshot: before}
	standby := newLifecycleController()
	defer standby.stop()
	standby.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: p, snapshot: before}
	start := time.Now()
	ownerRound := owner.runOnce(ctx, policy.Subject)
	ownerElapsed := time.Since(start)
	require.ErrorIs(t, ownerRound, context.DeadlineExceeded)
	ownerCapacityCalls := p.capacityCalls.Load()
	ownerManager := &MQManager{current: r, lifecycle: owner}
	ownerPublish := ownerManager.Publish(ctx, policy.Subject, []byte("Core合成容量诊断"), nil)
	available, e := r.inspectLifecycleCapacity(ctx, policy)
	require.NoError(t, e)
	require.NotNil(t, available.RetainedBytes)
	require.Less(t, *available.RetainedBytes, policy.Capacity.HardBytes)
	start = time.Now()
	standbyRound := standby.runOnce(ctx, policy.Subject)
	standbyElapsed := time.Since(start)
	require.NoError(t, standbyRound)
	standbyManager := &MQManager{current: r, lifecycle: standby}
	require.NoError(t, standbyManager.Publish(ctx, policy.Subject, []byte("Core合成容量诊断"), nil))
	require.EqualValues(t, 0, p.reclaimCalls.Load())
	require.EqualValues(t, 1, p.fullCalls.Load())
	require.Equal(t, policy.Fingerprint(), r.client.HGet(ctx, r.lifecycleMetaKey(policy.Subject), "fingerprint").Val())
	fullRuntime, _ := owner.runtimeMetricSnapshot(ctx)
	partialRuntime, _ := standby.runtimeMetricSnapshot(ctx)
	t.Logf("真实Redis owner_round=%v owner_elapsed=%s owner_capacity_calls=%d owner_publish=%v; standby_elapsed=%s total_light_calls=%d standby_publish=nil; direct_capacity_bytes=%d hard=%d fingerprint_unchanged=true reclaim=0", ownerRound, ownerElapsed, ownerCapacityCalls, ownerPublish, standbyElapsed, p.capacityCalls.Load(), *available.RetainedBytes, policy.Capacity.HardBytes)
	t.Logf("owner_runtime_state=%s owner_retained=%v standby_state=%s standby_retained=%v deadline=%v", fullRuntime.State, fullRuntime.Gauges, partialRuntime.State, partialRuntime.Gauges, fullRuntime.Counters["reclaim_fail_deadline_total"])
	require.EqualValues(t, 1, ownerCapacityCalls)
	require.EqualValues(t, 2, p.capacityCalls.Load())
	require.Equal(t, before, owner.entries[policy.Subject].snapshot)
	require.NoError(t, ownerPublish, "CAPACITY_REFRESH_OWNER_GAP: 真实Redis轻量容量可用，但owner完整检查超时没有刷新")
}

// TestLifecycleCapacityRejectReasons是既有Runtime聚合面的加性原因契约；原代码缺少固定原因，先保留RED。
func TestLifecycleCapacityRejectReasons(t *testing.T) {
	c := newLifecycleController()
	defer c.stop()
	policy := validLifecyclePolicy("required")
	policy.Reclaim.Interval = 2 * time.Second
	policy.Capacity.HardBytes = 10
	n := int64(10)
	cases := []struct {
		name     string
		s        LifecycleSnapshot
		messages bool
		want     error
	}{
		{"not_observed", LifecycleSnapshot{State: LifecycleStateOK}, false, ErrLifecycleCapacityUnknown},
		{"stale", LifecycleSnapshot{State: LifecycleStateOK, ObservedAt: time.Now().Add(-5 * time.Second)}, false, ErrLifecycleCapacityUnknown},
		{"state", LifecycleSnapshot{State: LifecycleStateUnavailable, ObservedAt: time.Now()}, false, ErrLifecycleCapacityUnknown},
		{"messages_missing", LifecycleSnapshot{State: LifecycleStateOK, ObservedAt: time.Now()}, true, ErrLifecycleCapacityUnknown},
		{"bytes_missing", LifecycleSnapshot{State: LifecycleStateOK, ObservedAt: time.Now()}, false, ErrLifecycleCapacityUnknown},
		{"hard_reached", LifecycleSnapshot{State: LifecycleStateOK, ObservedAt: time.Now(), RetainedBytes: &n}, false, ErrLifecycleBackpressure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := policy
			if tc.messages {
				p.Capacity.HardMessages = 10
				p.Capacity.HardBytes = 0
			}
			c.mu.Lock()
			c.entries[p.Subject] = &lifecycleEntry{policy: p, snapshot: tc.s}
			c.mu.Unlock()
			require.ErrorIs(t, c.allowPublish(p.Subject), tc.want)
			m, _ := c.runtimeMetricSnapshot(context.Background())
			require.EqualValues(t, 1, m.Counters["publish_rejected_reason_"+tc.name+"_total"], "必须可区分固定低基数拒绝原因")
		})
	}
}
