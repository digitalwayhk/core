// 本文件覆盖新模式的失效边界；真实SQL和跨进程另行验证，不把内存工作源当业务事实。
package ownership_test

import (
	"context"
	"errors"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGroupWorkContinuousBeyondExecution使用原30s单元预算，持续健康组超过该时长仍保有同一归属。
func TestGroupWorkContinuousBeyondExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("NOT_RUN: native 30s execution boundary")
	}
	store, m := groupWorkFixture(t, ownership.Timing{})
	budget := budgetForTest(t, 2, 4)
	ctx := context.Background()
	hold := make(chan struct{})
	deadlineSeen := make(chan struct{})
	var once sync.Once
	var healthy atomic.Int64
	runtime, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "boot", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, GroupWork: &ownership.GroupWorkOptions{Budget: budget, NextGroups: func(_ context.Context, _ ownership.Credential, q ownership.GroupWorkRequest) ([]string, error) {
		out := []string{}
		for _, g := range []string{"A", "B"} {
			excluded := false
			for _, x := range q.ExcludedGroups {
				if x == g {
					excluded = true
				}
			}
			if !excluded && len(out) < q.Limit {
				out = append(out, g)
			}
		}
		return out, nil
	}, StepGroup: func(ctx context.Context, _ ownership.Credential, g string) (bool, error) {
		if g == "A" {
			select {
			case <-ctx.Done():
				close(deadlineSeen)
				<-hold
				return false, ctx.Err()
			case <-hold:
				return true, nil
			}
		}
		timer := time.NewTimer(10 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-timer.C:
			healthy.Add(1)
			return true, nil
		}
	}}, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
		return ownership.Observation{At: time.Now(), Due: 2}, nil
	}})
	require.NoError(t, err)
	t.Cleanup(func() { once.Do(func() { close(hold) }); require.NoError(t, runtime.Close(ctx)) })
	var original ownership.Credential
	require.Eventually(t, func() bool { var e error; original, e = store.Resolve(ctx, m.Scope, "bucket"); return e == nil }, 3*time.Second, time.Millisecond)
	select {
	case <-deadlineSeen:
	case <-time.After(m.Timing.Execution + 3*time.Second):
		t.Fatal("单元没有得到原Execution取消")
	}
	before := healthy.Load()
	time.Sleep(2 * m.Timing.Renew)
	after, err := store.Resolve(ctx, m.Scope, "bucket")
	require.NoError(t, err)
	require.True(t, after.Same(original))
	require.Greater(t, healthy.Load(), before, "健康组不能因整轮超过30s而停止")
	status, err := runtime.Status(ctx, "bucket")
	require.NoError(t, err)
	require.NotNil(t, status.LastConfirmed)
	require.Positive(t, budget.RuntimeMetricSnapshot(ctx).Counters["ownership_work_timeouts_total"])
	require.Zero(t, runtime.RuntimeMetricSnapshot(ctx).Counters["ownership_execution_revoked_total"], "工作单元取消不是整个排空循环超时")
	once.Do(func() { close(hold) })
}

// TestGroupWorkPausedFailureDoesNotBlockHealthy错误组由应用持久分类的等价内存源暂停，Core不跳前序。
func TestGroupWorkPausedFailureDoesNotBlockHealthy(t *testing.T) {
	store, m := groupWorkFixture(t, fastGroupTiming())
	budget := budgetForTest(t, 2, 4)
	ctx := context.Background()
	var paused atomic.Bool
	var aCalls, bCalls atomic.Int64
	runtime, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "boot", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, GroupWork: &ownership.GroupWorkOptions{Budget: budget, NextGroups: func(_ context.Context, _ ownership.Credential, q ownership.GroupWorkRequest) ([]string, error) {
		groups := []string{}
		for _, g := range []string{"A", "B"} {
			if g == "A" && paused.Load() || g == "B" && bCalls.Load() >= 3 {
				continue
			}
			skip := false
			for _, x := range q.ExcludedGroups {
				if x == g {
					skip = true
				}
			}
			if !skip && len(groups) < q.Limit {
				groups = append(groups, g)
			}
		}
		return groups, nil
	}, StepGroup: func(_ context.Context, _ ownership.Credential, g string) (bool, error) {
		if g == "A" {
			aCalls.Add(1)
			paused.Store(true)
			return false, errors.New("synthetic persisted pause")
		}
		bCalls.Add(1)
		return true, nil
	}}, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
		return ownership.Observation{At: time.Now(), Due: 4 - bCalls.Load(), BlockedDue: 1}, nil
	}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close(ctx)) })
	require.Eventually(t, func() bool {
		return bCalls.Load() == 3 && budget.RuntimeMetricSnapshot(ctx).Gauges["ownership_work_reserved"] == 0
	}, time.Second, time.Millisecond)
	require.EqualValues(t, 1, aCalls.Load())
	s, err := runtime.Status(ctx, "bucket")
	require.NoError(t, err)
	require.NotNil(t, s.LastConfirmed)
	time.Sleep(m.Timing.Stall + 2*m.Timing.Renew)
	after, err := runtime.Status(ctx, "bucket")
	require.NoError(t, err)
	require.Equal(t, *s.LastConfirmed, *after.LastConfirmed, "暂停/查询不能假确认")
	require.Equal(t, s.Epoch, after.Epoch)
	require.EqualValues(t, 1, after.Observation.Due)
	require.Positive(t, budget.RuntimeMetricSnapshot(ctx).Counters["ownership_work_errors_total"])
}

// TestGroupWorkRejectsBadSelection非法发现不执行，不泄漏预取额度或伪造确认。
func TestGroupWorkRejectsBadSelection(t *testing.T) {
	for _, kind := range []string{"empty", "oversized", "duplicate", "too_many", "panic"} {
		t.Run(kind, func(t *testing.T) {
			store, m := groupWorkFixture(t, fastGroupTiming())
			budget := budgetForTest(t, 2, 2)
			ctx := context.Background()
			var calls atomic.Int64
			next := func(context.Context, ownership.Credential, ownership.GroupWorkRequest) ([]string, error) {
				switch kind {
				case "empty":
					return []string{""}, nil
				case "oversized":
					return []string{strings.Repeat("x", 513)}, nil
				case "duplicate":
					return []string{"A", "A"}, nil
				case "too_many":
					return []string{"A", "B", "C"}, nil
				default:
					panic("synthetic")
				}
			}
			runtime, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "boot", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, GroupWork: &ownership.GroupWorkOptions{Budget: budget, NextGroups: next, StepGroup: func(context.Context, ownership.Credential, string) (bool, error) { calls.Add(1); return true, nil }}, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
				return ownership.Observation{At: time.Now(), Due: 1}, nil
			}})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, runtime.Close(ctx)) })
			require.Eventually(t, func() bool {
				return budget.RuntimeMetricSnapshot(ctx).Counters["ownership_work_selection_errors_total"] >= 2
			}, time.Second, time.Millisecond)
			require.Zero(t, calls.Load())
			require.Zero(t, budget.RuntimeMetricSnapshot(ctx).Counters["ownership_work_completed_total"])
			require.NoError(t, runtime.Close(ctx))
			require.Zero(t, budget.RuntimeMetricSnapshot(ctx).Gauges["ownership_work_reserved"])
		})
	}
}

// TestGroupWorkBudgetValidatesExplicitBounds旧零值不创建预算，新声明必须有效且身份一致。
func TestGroupWorkBudgetValidatesExplicitBounds(t *testing.T) {
	for _, o := range []ownership.GroupWorkBudgetOptions{{}, {Workers: 65, Prefetch: 65}, {Workers: 2, Prefetch: 1}, {Workers: 1, Prefetch: 1025}} {
		b, err := ownership.NewGroupWorkBudget("owner", o)
		require.Error(t, err)
		require.Nil(t, b)
	}
	b, err := ownership.NewGroupWorkBudget("", ownership.GroupWorkBudgetOptions{Workers: 1, Prefetch: 1})
	require.Error(t, err)
	require.Nil(t, b)
	store, m := groupWorkFixture(t, fastGroupTiming())
	budget := budgetForTest(t, 1, 1)
	ctx := context.Background()
	work := &ownership.GroupWorkOptions{Budget: budget, NextGroups: func(context.Context, ownership.Credential, ownership.GroupWorkRequest) ([]string, error) {
		return nil, nil
	}, StepGroup: func(context.Context, ownership.Credential, string) (bool, error) { return false, nil }}
	base := ownership.Options{Manifest: m, InstanceID: "other", SessionID: "boot", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
		return ownership.Observation{At: time.Now()}, nil
	}, GroupWork: work}
	r, err := ownership.StartRuntime(ctx, store, base)
	require.Error(t, err)
	require.Nil(t, r)
	base.InstanceID = "owner"
	base.Step = func(context.Context, ownership.Credential) (bool, error) { return false, nil }
	r, err = ownership.StartRuntime(ctx, store, base)
	require.Error(t, err)
	require.Nil(t, r)
}
