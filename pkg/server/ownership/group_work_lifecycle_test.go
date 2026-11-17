// 本文件验证失去Ready、维护恢复与真实退出边界；不以取消信号代替业务回调退出。
package ownership_test

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroupWorkLateCompletionAfterFencing(t *testing.T) {
	store, m := groupWorkFixture(t, fastGroupTiming())
	ctx := context.Background()
	budget := budgetForTest(t, 1, 2)
	entered := make(chan struct{})
	hold := make(chan struct{})
	var once sync.Once
	rt, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "old", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, GroupWork: &ownership.GroupWorkOptions{Budget: budget, NextGroups: func(context.Context, ownership.Credential, ownership.GroupWorkRequest) ([]string, error) {
		return []string{"A"}, nil
	}, StepGroup: func(context.Context, ownership.Credential, string) (bool, error) {
		close(entered)
		<-hold
		return true, nil
	}}, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
		return ownership.Observation{At: time.Now(), Due: 1}, nil
	}})
	require.NoError(t, err)
	t.Cleanup(func() { once.Do(func() { close(hold) }); require.NoError(t, rt.Close(ctx)) })
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("工作未进入")
	}
	old, err := store.Resolve(ctx, m.Scope, "bucket")
	require.NoError(t, err)
	require.NoError(t, store.Release(ctx, old))
	newer, err := store.Acquire(ctx, ownership.Credential{Scope: m.Scope, Key: "bucket", Service: m.Service, InstanceID: "new", SessionID: "new"}, m.Timing.Lease)
	require.NoError(t, err)
	require.NoError(t, store.Ready(ctx, newer))
	once.Do(func() { close(hold) })
	require.Eventually(t, func() bool { return budget.RuntimeMetricSnapshot(ctx).Gauges["ownership_work_reserved"] == 0 }, time.Second, time.Millisecond)
	require.Zero(t, budget.RuntimeMetricSnapshot(ctx).Counters["ownership_work_completed_total"])
	require.ErrorIs(t, store.Validate(ctx, old, true), ownership.ErrFenced)
	current, err := store.Resolve(ctx, m.Scope, "bucket")
	require.NoError(t, err)
	require.True(t, current.Same(newer))
}
func TestGroupWorkRestoreWaitsOnlyItsActualJobs(t *testing.T) {
	store, m := groupWorkFixture(t, fastGroupTiming())
	ctx := context.Background()
	budget := budgetForTest(t, 2, 3)
	hold := make(chan struct{})
	entered := make(chan struct{})
	restored := make(chan struct{})
	var once sync.Once
	var selection atomic.Int64
	var restoreOverlap atomic.Bool
	var active atomic.Bool
	rt, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "restore", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, Restore: func(_ context.Context, _ ownership.Credential, snapshot ownership.ReferenceSnapshot) error {
		if len(snapshot.References) == 0 {
			return nil
		}
		if active.Load() {
			restoreOverlap.Store(true)
		}
		close(restored)
		return nil
	}, GroupWork: &ownership.GroupWorkOptions{Budget: budget, NextGroups: func(context.Context, ownership.Credential, ownership.GroupWorkRequest) ([]string, error) {
		if selection.Add(1) == 1 {
			return []string{"A"}, nil
		}
		return nil, nil
	}, StepGroup: func(context.Context, ownership.Credential, string) (bool, error) {
		active.Store(true)
		close(entered)
		<-hold
		active.Store(false)
		return true, nil
	}}, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
		return ownership.Observation{At: time.Now(), Due: 1}, nil
	}})
	require.NoError(t, err)
	t.Cleanup(func() { once.Do(func() { close(hold) }); require.NoError(t, rt.Close(ctx)) })
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("工作未进入")
	}
	require.NoError(t, store.OpenGateway(ctx, "gateway", m.Timing.Lease))
	require.NoError(t, store.ReplaceGateway(ctx, ownership.GatewaySnapshot{GatewayID: "gateway", Revision: 1, Lease: m.Timing.Lease, References: []ownership.ScopedReference{{Scope: m.Scope, Key: "bucket", SessionID: "connection", Name: "resource"}}}))
	select {
	case <-restored:
		t.Fatal("旧回调未退出就开始引用恢复")
	case <-time.After(30 * time.Millisecond):
	}
	once.Do(func() { close(hold) })
	select {
	case <-restored:
	case <-time.After(time.Second):
		t.Fatal("旧组退出后未恢复真实revision")
	}
	require.False(t, restoreOverlap.Load())
}
func TestGroupWorkRejectsOtherServiceBudgetAtStart(t *testing.T) {
	store, m := groupWorkFixture(t, fastGroupTiming())
	b := budgetForTest(t, 1, 1)
	ctx := context.Background()
	opts := ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "first", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
		return ownership.Observation{At: time.Now()}, nil
	}, GroupWork: &ownership.GroupWorkOptions{Budget: b, NextGroups: func(context.Context, ownership.Credential, ownership.GroupWorkRequest) ([]string, error) {
		return nil, nil
	}, StepGroup: func(context.Context, ownership.Credential, string) (bool, error) { return false, nil }}}
	rt, err := ownership.StartRuntime(ctx, store, opts)
	require.NoError(t, err)
	defer rt.Close(ctx)
	opts.Manifest.Service = "another-service"
	opts.Manifest.Group = "another-service"
	_, err = ownership.StartRuntime(ctx, store, opts)
	require.ErrorIs(t, err, ownership.ErrUnknown, "逻辑服务冲突必须启动同步失败，不能后台重试空转")
}

// 取消排队身份后不得进入业务查询；真实未退出的第一个工作仍占原worker。
func TestGroupWorkQueuedCancelDoesNotEnterHandler(t *testing.T) {
	store, m := groupWorkFixture(t, fastGroupTiming())
	ctx := context.Background()
	b := budgetForTest(t, 1, 3)
	hold := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	var calls atomic.Int64
	rt, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "queued", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, GroupWork: &ownership.GroupWorkOptions{Budget: b, NextGroups: func(_ context.Context, _ ownership.Credential, q ownership.GroupWorkRequest) ([]string, error) {
		excluded := map[string]bool{}
		for _, g := range q.ExcludedGroups {
			excluded[g] = true
		}
		for _, g := range []string{"A", "B", "C"} {
			if !excluded[g] {
				return []string{g}, nil
			}
		}
		return nil, nil
	}, StepGroup: func(context.Context, ownership.Credential, string) (bool, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-hold
		}
		return true, nil
	}}, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
		return ownership.Observation{At: time.Now(), Due: 3}, nil
	}})
	require.NoError(t, err)
	t.Cleanup(func() { once.Do(func() { close(hold) }); require.NoError(t, rt.Close(ctx)) })
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("首工作未进入")
	}
	require.Eventually(t, func() bool {
		s := b.RuntimeMetricSnapshot(ctx)
		return s.Gauges["ownership_work_reserved"] == 3 && s.Gauges["ownership_work_queued"] == 2
	}, time.Second, time.Millisecond)
	wait, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, b.Close(wait), context.DeadlineExceeded)
	require.EqualValues(t, 1, calls.Load())
	once.Do(func() { close(hold) })
	require.NoError(t, rt.Close(ctx))
	require.NoError(t, b.Close(ctx))
	require.EqualValues(t, 1, calls.Load())
	s := b.RuntimeMetricSnapshot(ctx)
	require.Zero(t, s.Gauges["ownership_work_reserved"])
	require.Zero(t, s.Gauges["ownership_work_queued"])
	require.Zero(t, s.Counters["ownership_work_completed_total"])
	opts := ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "closed", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
		return ownership.Observation{}, nil
	}, GroupWork: &ownership.GroupWorkOptions{Budget: b, NextGroups: func(context.Context, ownership.Credential, ownership.GroupWorkRequest) ([]string, error) {
		return nil, nil
	}, StepGroup: func(context.Context, ownership.Credential, string) (bool, error) { return false, nil }}}
	_, err = ownership.StartRuntime(ctx, store, opts)
	require.ErrorIs(t, err, ownership.ErrNotReady)
}
