// 本文件验证生命周期轮次所有权前置和独立阶段预算，不替代真实 Broker 契约。
package mq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type workerTestProvider struct {
	lifecycleTestProvider
	owner   bool
	delay   time.Duration
	expired atomic.Bool
}

type scheduledLifecycleProvider struct {
	workerTestProvider
	mu      sync.Mutex
	seen    map[string]bool
	started chan<- time.Time
}

func (p *scheduledLifecycleProvider) InspectLifecycle(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
	p.mu.Lock()
	first := !p.seen[policy.Subject]
	p.seen[policy.Subject] = true
	p.mu.Unlock()
	if first {
		p.started <- time.Now()
	}
	return p.workerTestProvider.InspectLifecycle(ctx, policy)
}

// TestLifecycleRegisteredWorkersSpreadStartup 通过真实 worker 定时器验证 13×44 次首次扫描不在同一时间点触发。
func TestLifecycleRegisteredWorkersSpreadStartup(t *testing.T) {
	started := make(chan time.Time, 13*44)
	for instance := 0; instance < 13; instance++ {
		c := newLifecycleController()
		defer c.stop()
		p := &scheduledLifecycleProvider{workerTestProvider: workerTestProvider{owner: true}, seen: make(map[string]bool), started: started}
		for subject := 0; subject < 44; subject++ {
			policy := validLifecyclePolicy("required")
			policy.Subject = fmt.Sprintf("fills-%d", subject)
			policy.Reclaim.Interval = 200 * time.Millisecond
			policy.Reclaim.TimeBudget = 100 * time.Millisecond
			c.mu.Lock()
			c.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: p}
			c.mu.Unlock()
			c.wg.Add(1)
			go c.runWorker(policy.Subject, policy.Reclaim.Interval)
		}
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	var first, last time.Time
	for i := 0; i < 13*44; i++ {
		select {
		case stamp := <-started:
			if first.IsZero() || stamp.Before(first) {
				first = stamp
			}
			if stamp.After(last) {
				last = stamp
			}
		case <-timer.C:
			t.Fatal("workers did not start within bounded jitter window")
		}
	}
	require.Greater(t, last.Sub(first), 100*time.Millisecond, "first scans must not share one phase")
}

// TestLifecycleWorkerFailureReasons 验证固定原因计数不会丢失 deadline，且保留总计数。
func TestLifecycleWorkerFailureReasons(t *testing.T) {
	p := &workerTestProvider{owner: true, delay: 90 * time.Millisecond}
	c := newLifecycleController()
	defer c.stop()
	policy := validLifecyclePolicy("required")
	policy.Reclaim.TimeBudget = 100 * time.Millisecond
	c.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: p}
	require.True(t, errors.Is(c.runOnce(context.Background(), policy.Subject), context.DeadlineExceeded))
	snapshot, _ := c.runtimeMetricSnapshot(context.Background())
	require.Equal(t, float64(1), snapshot.Counters["reclaim_fail_total"])
	require.Equal(t, float64(1), snapshot.Counters["reclaim_fail_deadline_total"])
}

// TestLifecycleNonOwnerMetricsDoNotRefreshOldPending 验证容量采集不伪装完整快照新鲜。
func TestLifecycleNonOwnerMetricsDoNotRefreshOldPending(t *testing.T) {
	p := &workerTestProvider{}
	c := newLifecycleController()
	defer c.stop()
	policy := validLifecyclePolicy("required")
	pending := int64(7)
	c.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: p, snapshot: LifecycleSnapshot{State: LifecycleStateOK, ObservedAt: time.Now().Add(-3 * policy.Reclaim.Interval), PendingMessages: &pending}}
	require.NoError(t, c.runOnce(context.Background(), policy.Subject))
	snapshot, _ := c.runtimeMetricSnapshot(context.Background())
	require.NotContains(t, snapshot.Gauges, "pending_messages")
	require.Equal(t, float64(1), snapshot.Gauges["retained_messages"])
}

func (p *workerTestProvider) acquireLifecycleRound(ctx context.Context, _ LifecyclePolicy, _ string) (context.Context, bool, error) {
	return ctx, p.owner, nil
}
func (p *workerTestProvider) inspectLifecycleCapacity(context.Context, LifecyclePolicy) (LifecycleSnapshot, error) {
	count := int64(1)
	return LifecycleSnapshot{ObservedAt: time.Now(), State: LifecycleStatePartial, RetainedMessages: &count}, nil
}
func (p *workerTestProvider) InspectLifecycle(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
	if p.delay > 0 {
		select {
		case <-ctx.Done():
			return LifecycleSnapshot{}, ctx.Err()
		case <-time.After(p.delay):
		}
	}
	return p.lifecycleTestProvider.InspectLifecycle(ctx, policy)
}
func (p *workerTestProvider) ReclaimLifecycle(ctx context.Context, policy LifecyclePolicy, snapshot LifecycleSnapshot) (ReclaimResult, error) {
	p.expired.Store(ctx.Err() != nil)
	return p.lifecycleTestProvider.ReclaimLifecycle(ctx, policy, snapshot)
}

// TestLifecycleNonOwnerSkipsInspect 验证非 owner 只刷新容量，不执行组扫描。
func TestLifecycleNonOwnerSkipsInspect(t *testing.T) {
	p := &workerTestProvider{}
	c := newLifecycleController()
	defer c.stop()
	policy := validLifecyclePolicy("required")
	policy.Capacity.HardMessages = 10
	c.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: p}
	require.NoError(t, c.runOnce(context.Background(), policy.Subject))
	require.Zero(t, p.inspectCalls, "non-owner must not inspect groups")
	require.Zero(t, p.reclaimCalls)
	require.NoError(t, c.allowPublish(policy.Subject))
}

// TestLifecycleSlowInspectPreservesRoundBound 验证慢检查被提前终止，不吞掉回收预留时间。
func TestLifecycleSlowInspectPreservesRoundBound(t *testing.T) {
	p := &workerTestProvider{owner: true, delay: 90 * time.Millisecond}
	c := newLifecycleController()
	defer c.stop()
	policy := validLifecyclePolicy("required")
	policy.Reclaim.TimeBudget = 100 * time.Millisecond
	c.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: p}
	start := time.Now()
	err := c.runOnce(context.Background(), policy.Subject)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, p.reclaimCalls)
	require.Less(t, time.Since(start), 120*time.Millisecond)
}

// TestLifecycleReclaimHasIndependentContext 验证检查阶段取消不传播至回收阶段。
func TestLifecycleReclaimHasIndependentContext(t *testing.T) {
	p := &workerTestProvider{owner: true, delay: 10 * time.Millisecond}
	c := newLifecycleController()
	defer c.stop()
	policy := validLifecyclePolicy("required")
	policy.Reclaim.TimeBudget = 100 * time.Millisecond
	c.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: p}
	require.NoError(t, c.runOnce(context.Background(), policy.Subject))
	require.Equal(t, 1, p.reclaimCalls)
	require.False(t, p.expired.Load())
}

// TestLifecycleWorkerJitterSpread 验证启动和周期都不是固定同相位，且延迟有界。
func TestLifecycleWorkerJitterSpread(t *testing.T) {
	for _, startup := range []bool{true, false} {
		buckets := make(map[int64]int)
		for i := 0; i < 13*44; i++ {
			delay := lifecycleWorkerDelay(2*time.Second, startup)
			require.Greater(t, delay, time.Duration(0))
			require.Less(t, delay, 2500*time.Millisecond)
			buckets[delay.Milliseconds()/100]++
		}
		require.GreaterOrEqual(t, len(buckets), 8, "jitter must spread registration bursts")
	}
}
