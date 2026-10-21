// 本文件验证持续运行时的空闲/停滞/取消边界，不能替代独立进程验收。
package mq

import (
	"context"
	"errors"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestRedisOwnershipRuntimeSplitsKeysAndKeepsIdle(t *testing.T) {
	_, s, m := ownershipFixture(t)
	ctx := context.Background()
	started := make(chan ownership.Credential, 4)
	options := func(id string) ownership.Options {
		return ownership.Options{Manifest: m, InstanceID: id, SessionID: id + "-boot", MaxOwned: 1, Activate: func(_ context.Context, c ownership.Credential) error { started <- c; return nil }, Step: func(context.Context, ownership.Credential) (bool, error) { return false, nil }, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
			return ownership.Observation{At: time.Now()}, nil
		}}
	}
	a, err := ownership.StartRuntime(ctx, s, options("a"))
	require.NoError(t, err)
	defer a.Close(ctx)
	b, err := ownership.StartRuntime(ctx, s, options("b"))
	require.NoError(t, err)
	defer b.Close(ctx)
	var claims []ownership.Credential
	for len(claims) < 2 {
		select {
		case c := <-started:
			claims = append(claims, c)
		case <-time.After(3 * time.Second):
			t.Fatal("两个实例未各取得一个独立键")
		}
	}
	require.NotEqual(t, claims[0].Key, claims[1].Key)
	require.NotEqual(t, claims[0].InstanceID, claims[1].InstanceID)
	for _, c := range claims {
		require.Eventually(t, func() bool { current, e := s.Resolve(ctx, c.Scope, c.Key); return e == nil && current.Same(c) }, time.Second, time.Millisecond)
		owner := a
		nonowner := b
		if c.InstanceID == "b" {
			owner = b
			nonowner = a
		}
		status, err := owner.Status(ctx, c.Key)
		require.NoError(t, err)
		require.Equal(t, c.Epoch, status.Epoch)
		require.Nil(t, status.LastConfirmed, "空闲不能伪造业务确认进度")
		require.Zero(t, status.Observation.Due)
		_, err = nonowner.Status(ctx, c.Key)
		require.ErrorIs(t, err, ownership.ErrNotReady)
	}
}

func TestRedisOwnershipRuntimeTimeoutWaitsForRealHandler(t *testing.T) {
	p := newPoolBudgetProvider(t, 4, 8)
	ctx := context.Background()
	m := ownership.Manifest{Scope: "timeout", Service: "quotes", Group: "quotes", Selector: "market-v1", Subjects: []string{"one"}, Keys: []string{"A"}, Timing: ownership.Timing{Lease: 600 * time.Millisecond, Renew: 100 * time.Millisecond, Poll: 10 * time.Millisecond, Execution: 200 * time.Millisecond, Stall: 300 * time.Millisecond, Observation: 150 * time.Millisecond}}.Normalize()
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	s, err := p.OwnershipStore(ctx)
	require.NoError(t, err)
	entered, hold := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	defer release()
	r, err := ownership.StartRuntime(ctx, s, ownership.Options{Manifest: m, InstanceID: "old", SessionID: "old-boot", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, Step: func(context.Context, ownership.Credential) (bool, error) { close(entered); <-hold; return true, nil }, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
		return ownership.Observation{At: time.Now(), Delivered: 1}, nil
	}})
	require.NoError(t, err)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler未开始")
	}
	t.Cleanup(func() { release(); _ = r.Close(ctx) })
	require.Eventually(t, func() bool { _, err := s.Resolve(ctx, m.Scope, "A"); return errors.Is(err, ownership.ErrNotReady) }, time.Second, time.Millisecond, "执行超时后不能仍公布Ready owner")
	require.Zero(t, r.RuntimeMetricSnapshot(ctx).Gauges["ownership_ready"], "已撤销租约不能因旧handler尚未退出继续计为Ready")
	require.Positive(t, r.RuntimeMetricSnapshot(ctx).Gauges["ownership_inflight"], "不能把旧handler伪装为已退出")
	wait, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	require.ErrorIs(t, r.Close(wait), context.DeadlineExceeded, "不能遗弃不合作handler")
	release()
	require.NoError(t, r.Close(ctx))
}

type ownershipFailedRelease struct{ ownership.Store }

func (ownershipFailedRelease) Release(context.Context, ownership.Credential) error {
	return ownership.ErrUnknown
}

// Release命令未知时Broker旧lease可能尚在；本地已撤销key仍必须拒绝查询。
func TestRedisOwnershipLocallyRevokedKeyCannotServeDuringUnknownRelease(t *testing.T) {
	p := newPoolBudgetProvider(t, 4, 8)
	ctx := context.Background()
	m := ownership.Manifest{Scope: "local-revoke", Service: "quotes", Group: "quotes", Selector: "v1", Keys: []string{"A"}, Subjects: []string{"one"}, Timing: ownership.Timing{Lease: 900 * time.Millisecond, Renew: 100 * time.Millisecond, Poll: 10 * time.Millisecond, Execution: 200 * time.Millisecond, Stall: 400 * time.Millisecond, Observation: 200 * time.Millisecond}}.Normalize()
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	store, err := p.OwnershipStore(ctx)
	require.NoError(t, err)
	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	defer release()
	runtime, err := ownership.StartRuntime(ctx, ownershipFailedRelease{store}, ownership.Options{Manifest: m, InstanceID: "old", SessionID: "old-boot", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, Step: func(context.Context, ownership.Credential) (bool, error) { <-hold; return true, nil }, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
		return ownership.Observation{At: time.Now(), Delivered: 1}, nil
	}})
	require.NoError(t, err)
	t.Cleanup(func() { release(); _ = runtime.Close(ctx) })
	require.Eventually(t, func() bool { return runtime.RuntimeMetricSnapshot(ctx).Gauges["ownership_ready"] == 1 }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return runtime.RuntimeMetricSnapshot(ctx).Gauges["ownership_ready"] == 0 }, time.Second, time.Millisecond)
	_, err = store.Resolve(ctx, m.Scope, "A")
	require.NoError(t, err, "控制注入必须保留Broker尚未过期的旧Ready租约")
	_, err = runtime.Status(ctx, "A")
	require.ErrorIs(t, err, ownership.ErrNotReady, "本地key已取消，不能继续返回旧视图")
}
