// 本文件用真实Redis和受控组首源验证独立完成、原前序及实例共用预算，不代替真实SQL/进程验收。
package ownership_test

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func groupWorkFixture(t *testing.T, timing ownership.Timing) (ownership.Store, ownership.Manifest) {
	t.Helper()
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: isolated CORE_TEST_REDIS_ADDR")
	}
	ctx := context.Background()
	prefix := fmt.Sprintf("group-work-%d", time.Now().UnixNano())
	p := mq.NewRedisStreamProviderWithConfig(config.RedisStreamMQConfig{Addr: addr, Prefix: prefix, CommandPoolSize: 8, BlockingReadPoolSize: 4})
	require.NoError(t, p.Connect(ctx))
	t.Cleanup(func() { require.NoError(t, p.Close()) })
	m := ownership.Manifest{Scope: prefix, Service: "fixture", Group: "fixture", Selector: "opaque", Keys: []string{"bucket"}, Subjects: []string{"input"}, Timing: timing}.Normalize()
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	store, err := p.OwnershipStore(ctx)
	require.NoError(t, err)
	return store, m
}
func fastGroupTiming() ownership.Timing {
	return ownership.Timing{Lease: 3 * time.Second, Renew: 100 * time.Millisecond, Poll: 5 * time.Millisecond, Execution: time.Second, Stall: 2 * time.Second, Observation: 300 * time.Millisecond}
}
func budgetForTest(t *testing.T, workers, prefetch int) *ownership.GroupWorkBudget {
	t.Helper()
	b, err := ownership.NewGroupWorkBudget("owner", ownership.GroupWorkBudgetOptions{Workers: workers, Prefetch: prefetch})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, b.Close(ctx))
	})
	return b
}

// TestGroupWorkBSuccessorDoesNotWaitForA证明B2在A1未退出时真正进入，同A后继未查询/claim。
func TestGroupWorkBSuccessorDoesNotWaitForA(t *testing.T) {
	store, m := groupWorkFixture(t, fastGroupTiming())
	budget := budgetForTest(t, 2, 4)
	ctx := context.Background()
	hold := make(chan struct{})
	b2 := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	remaining := map[string]int{"A": 2, "B": 2}
	queries := map[string]int{}
	active := map[string]int{}
	bad := false
	next := func(_ context.Context, _ ownership.Credential, q ownership.GroupWorkRequest) ([]string, error) {
		mu.Lock()
		defer mu.Unlock()
		excluded := map[string]bool{}
		for _, g := range q.ExcludedGroups {
			excluded[g] = true
		}
		groups := []string{}
		for _, g := range []string{"A", "B"} {
			if remaining[g] > 0 && !excluded[g] && len(groups) < q.Limit {
				groups = append(groups, g)
			}
		}
		return groups, nil
	}
	step := func(ctx context.Context, _ ownership.Credential, g string) (bool, error) {
		mu.Lock()
		queries[g]++
		n := queries[g]
		active[g]++
		if active[g] > 1 {
			bad = true
		}
		mu.Unlock()
		if g == "A" && n == 1 {
			select {
			case <-hold:
			case <-ctx.Done():
				mu.Lock()
				active[g]--
				mu.Unlock()
				return false, ctx.Err()
			}
		}
		if g == "B" && n == 2 {
			close(b2)
		}
		mu.Lock()
		active[g]--
		remaining[g]--
		mu.Unlock()
		return true, nil
	}
	runtime, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "boot", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, GroupWork: &ownership.GroupWorkOptions{Budget: budget, NextGroups: next, StepGroup: step}, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
		mu.Lock()
		n := remaining["A"] + remaining["B"]
		mu.Unlock()
		return ownership.Observation{At: time.Now(), Due: int64(n)}, nil
	}})
	require.NoError(t, err)
	t.Cleanup(func() { once.Do(func() { close(hold) }); require.NoError(t, runtime.Close(ctx)) })
	select {
	case <-b2:
	case <-time.After(700 * time.Millisecond):
		t.Fatal("B2仍被无关A1完成屏障阻塞")
	}
	mu.Lock()
	aQueries := queries["A"]
	concurrentSameGroup := bad
	mu.Unlock()
	require.Equal(t, 1, aQueries, "A2不能提前进入组首查询")
	require.False(t, concurrentSameGroup)
	once.Do(func() { close(hold) })
	require.Eventually(t, func() bool { return budget.RuntimeMetricSnapshot(ctx).Counters["ownership_work_completed_total"] == 4 }, 2*time.Second, time.Millisecond)
	status, err := runtime.Status(ctx, "bucket")
	require.NoError(t, err)
	require.NotNil(t, status.LastConfirmed)
}

// TestGroupWorkBudgetSharedAcrossRuntimes两个scope共用固定worker和身份上限，不把预算按scope相乘。
func TestGroupWorkBudgetSharedAcrossRuntimes(t *testing.T) {
	budget := budgetForTest(t, 2, 3)
	ctx := context.Background()
	var active, peak, completed atomic.Int64
	var bad atomic.Bool
	var runtimes []*ownership.Runtime
	for i := 0; i < 2; i++ {
		store, m := groupWorkFixture(t, fastGroupTiming())
		var mu sync.Mutex
		left := map[string]bool{}
		for n := 0; n < 12; n++ {
			left[fmt.Sprintf("group-%02d", n)] = true
		}
		next := func(_ context.Context, _ ownership.Credential, q ownership.GroupWorkRequest) ([]string, error) {
			mu.Lock()
			defer mu.Unlock()
			blocked := map[string]bool{}
			for _, g := range q.ExcludedGroups {
				blocked[g] = true
			}
			keys := []string{}
			for g := range left {
				if !blocked[g] {
					keys = append(keys, g)
				}
			}
			sort.Strings(keys)
			if len(keys) > q.Limit {
				keys = keys[:q.Limit]
			}
			return keys, nil
		}
		step := func(_ context.Context, _ ownership.Credential, g string) (bool, error) {
			n := active.Add(1)
			for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
			}
			if n > 2 || budget.RuntimeMetricSnapshot(ctx).Gauges["ownership_work_reserved"] > 3 {
				bad.Store(true)
			}
			time.Sleep(2 * time.Millisecond)
			mu.Lock()
			if !left[g] {
				bad.Store(true)
			}
			delete(left, g)
			mu.Unlock()
			active.Add(-1)
			completed.Add(1)
			return true, nil
		}
		runtime, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: "owner", SessionID: fmt.Sprintf("boot-%d", i), MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, GroupWork: &ownership.GroupWorkOptions{Budget: budget, NextGroups: next, StepGroup: step}, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
			mu.Lock()
			n := len(left)
			mu.Unlock()
			return ownership.Observation{At: time.Now(), Due: int64(n)}, nil
		}})
		require.NoError(t, err)
		runtimes = append(runtimes, runtime)
	}
	t.Cleanup(func() {
		for _, r := range runtimes {
			require.NoError(t, r.Close(ctx))
		}
	})
	require.Eventually(t, func() bool { return completed.Load() == 24 }, 3*time.Second, time.Millisecond)
	require.False(t, bad.Load())
	require.LessOrEqual(t, peak.Load(), int64(2))
	require.Eventually(t, func() bool { return budget.RuntimeMetricSnapshot(ctx).Gauges["ownership_work_reserved"] == 0 }, time.Second, time.Millisecond)
}

// TestGroupWorkCloseWaitsForRealExit取消不合作工作后，预算/Runtime都不能假称退出或替补worker。
func TestGroupWorkCloseWaitsForRealExit(t *testing.T) {
	store, m := groupWorkFixture(t, fastGroupTiming())
	budget := budgetForTest(t, 1, 1)
	ctx := context.Background()
	hold := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	runtime, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "boot", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, GroupWork: &ownership.GroupWorkOptions{Budget: budget, NextGroups: func(context.Context, ownership.Credential, ownership.GroupWorkRequest) ([]string, error) {
		return []string{"A"}, nil
	}, StepGroup: func(context.Context, ownership.Credential, string) (bool, error) {
		close(entered)
		<-hold
		return true, nil
	}}, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
		return ownership.Observation{At: time.Now(), Due: 1}, nil
	}})
	require.NoError(t, err)
	t.Cleanup(func() { once.Do(func() { close(hold) }); require.NoError(t, runtime.Close(ctx)) })
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("工作未执行")
	}
	status, err := runtime.Status(ctx, "bucket")
	require.NoError(t, err)
	require.EqualValues(t, 1, status.Observation.Delivered, "正式Status不能隐藏实际回调")
	wait, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, runtime.Close(wait), context.DeadlineExceeded)
	require.ErrorIs(t, budget.Close(wait), context.DeadlineExceeded)
	snap := budget.RuntimeMetricSnapshot(ctx)
	require.Equal(t, 1.0, snap.Gauges["ownership_work_active"])
	require.Zero(t, snap.Counters["ownership_work_completed_total"])
	once.Do(func() { close(hold) })
	require.NoError(t, runtime.Close(ctx))
	require.NoError(t, budget.Close(ctx))
	require.Zero(t, budget.RuntimeMetricSnapshot(ctx).Counters["ownership_work_completed_total"], "取消后迟到成功不能确认")
}
