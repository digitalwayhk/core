// 本文件以独占真实Redis检验多个Subject的容量补读命令增量及原500ms轮次预算，不作生产容量认证。
package mq

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRedisOwnerCapacityRefreshMultiSubject每个失败owner轮次只增加一组4命令管线，standby不执行全组检查。
func TestRedisOwnerCapacityRefreshMultiSubject(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: 独占CORE_TEST_REDIS_ADDR")
	}
	ctx := context.Background()
	r := NewRedisStreamProvider(addr, fmt.Sprintf("core:capacity-multi:%d", time.Now().UnixNano()), 0)
	require.NoError(t, r.Connect(ctx))
	defer r.Close()
	p := &capacityRefreshDiagnosticRedis{RedisStreamProvider: r}
	owner, standby := newLifecycleController(), newLifecycleController()
	defer owner.stop()
	defer standby.stop()
	const count = 8
	policies := make([]LifecyclePolicy, count)
	for i := range policies {
		policy := validLifecyclePolicy("required")
		policy.Subject = fmt.Sprintf("subject-%02d", i)
		policy.Reclaim.Interval = 2 * time.Second
		policy.Reclaim.TimeBudget = 500 * time.Millisecond
		policy.Capacity.HardBytes = 256 << 20
		require.NoError(t, r.EnsureLifecycle(ctx, policy))
		s, e := r.InspectLifecycle(ctx, policy)
		require.NoError(t, e)
		s.ObservedAt = time.Now().Add(-5 * time.Second)
		policies[i] = policy
		owner.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: p, snapshot: s}
		standby.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: p, snapshot: s}
	}
	before := workerRedisCommandCounts(t, r)
	started := time.Now()
	var slowest time.Duration
	for _, policy := range policies {
		stamp := time.Now()
		require.ErrorIs(t, owner.runOnce(ctx, policy.Subject), context.DeadlineExceeded)
		if d := time.Since(stamp); d > slowest {
			slowest = d
		}
		require.NoError(t, standby.runOnce(ctx, policy.Subject))
		for _, c := range []*lifecycleController{owner, standby} {
			m := &MQManager{current: r, lifecycle: c}
			require.NoError(t, m.Publish(ctx, policy.Subject, []byte("Core合成多Subject诊断"), nil))
		}
	}
	after := workerRedisCommandCounts(t, r)
	for _, command := range []string{"type", "xlen", "memory|usage"} {
		require.EqualValues(t, 2*count, after[command]-before[command], command)
		t.Logf("%s delta=%d", command, after[command]-before[command])
	}
	require.EqualValues(t, count, p.ownerLightCalls.Load())
	require.EqualValues(t, count, p.standbyLightCalls.Load())
	require.EqualValues(t, count, p.fullCalls.Load())
	require.Zero(t, p.reclaimCalls.Load())
	require.LessOrEqual(t, time.Duration(p.maxOwnerRemaining.Load()), 150*time.Millisecond)
	for _, policy := range policies {
		require.EqualValues(t, 2, r.client.XLen(ctx, r.streamKey(policy.Subject)).Val())
	}
	t.Logf("8Subject/2controller owner_light=%d standby_light=%d full=%d reclaim=0 max_owner_remaining=%s slowest_owner=%s total=%s", p.ownerLightCalls.Load(), p.standbyLightCalls.Load(), p.fullCalls.Load(), time.Duration(p.maxOwnerRemaining.Load()), slowest, time.Since(started))
}

// TestRedisOwnerCapacityRefreshExhaustsRemainingBudget真实Redis网络暂停使容量补读耗尽余量，仍unknown且不额外开500ms。
func TestRedisOwnerCapacityRefreshExhaustsRemainingBudget(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" || os.Getenv("CORE_TEST_LIFECYCLE_PAUSE_REDIS") != "1" {
		t.Skip("NOT_RUN: 独占Redis及显式允许暂停夹具")
	}
	ctx := context.Background()
	r := NewRedisStreamProvider(addr, fmt.Sprintf("core:capacity-budget:%d", time.Now().UnixNano()), 0)
	require.NoError(t, r.Connect(ctx))
	defer r.Close()
	policy := validLifecyclePolicy("required")
	policy.Reclaim.Interval = 2 * time.Second
	policy.Reclaim.TimeBudget = 500 * time.Millisecond
	policy.Capacity.HardBytes = 256 << 20
	require.NoError(t, r.EnsureLifecycle(ctx, policy))
	old, err := r.InspectLifecycle(ctx, policy)
	require.NoError(t, err)
	old.ObservedAt = time.Now().Add(-5 * time.Second)
	p := &capacityRefreshDiagnosticRedis{RedisStreamProvider: r}
	var pauseErr error
	p.afterFullTimeout = func() { pauseErr = r.client.Do(ctx, "CLIENT", "PAUSE", 300, "ALL").Err() }
	c := newLifecycleController()
	defer c.stop()
	entry := &lifecycleEntry{policy: policy, provider: p, snapshot: old}
	c.entries[policy.Subject] = entry
	started := time.Now()
	roundErr := c.runOnce(ctx, policy.Subject)
	elapsed := time.Since(started)
	require.NoError(t, pauseErr)
	require.ErrorIs(t, roundErr, context.DeadlineExceeded)
	require.ErrorIs(t, c.allowPublish(policy.Subject), ErrLifecycleCapacityUnknown)
	require.EqualValues(t, 1, p.ownerLightCalls.Load())
	require.LessOrEqual(t, time.Duration(p.maxOwnerRemaining.Load()), 150*time.Millisecond)
	require.Equal(t, old, entry.snapshot)
	require.Zero(t, p.reclaimCalls.Load())
	require.EqualValues(t, 1, entry.diagnostics.refreshFailures)
	require.Equal(t, "deadline", entry.diagnostics.capacityReadError)
	t.Logf("实际Redis暂停300ms：原总预算500ms 实际轮次=%s 补读进入时余量=%s 完整错误保留/unknown/reclaim0", elapsed, time.Duration(p.maxOwnerRemaining.Load()))
}
