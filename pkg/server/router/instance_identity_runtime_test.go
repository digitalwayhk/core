// 本文件验证就绪期限从请求发起时刻计算，暂停或断连不会虚增租约。
package router

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/identity"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestStableAdmissionUsesRequestStartAndRecovers(t *testing.T) {
	r := &stableIdentityRuntime{external: true, ttl: time.Second}
	r.running.Store(true)
	r.observe(time.Now().Add(-time.Second), nil)
	require.ErrorIs(t, r.admission(), identity.ErrAdmissionUnavailable)
	r.observe(time.Now(), nil)
	require.NoError(t, r.admission())
	r.observe(time.Now(), identity.ErrAdmissionUnavailable)
	require.Error(t, r.admission())
	r.observe(time.Now(), nil)
	require.NoError(t, r.admission())
	r.observe(time.Now().Add(time.Hour), nil)
	require.Error(t, r.admission(), "墙钟回拨不能仍然使用旧确认")
	r.observe(time.Now(), nil)
	r.beginStop()
	require.Error(t, r.admission())
}

// TestProcessReadinessKeepsFencedFailure 验证清理上下文不能把故障进程的探针变回成功。
func TestProcessReadinessKeepsFencedFailure(t *testing.T) {
	registry := newServiceContextRegistry()
	sc := &ServiceContext{stableIdentity: &stableIdentityRuntime{}}
	sc.stableIdentity.running.Store(true)
	registry.contexts["orders"] = sc
	require.NoError(t, registry.checkAdmission())
	sc.setRuntimeError(identity.ErrOwnerConflict)
	sc.stableIdentity.beginStop()
	require.True(t, registry.remove("orders", sc))
	require.Error(t, registry.checkAdmission(), "清理故障上下文后仍需返回未就绪")
	next := &ServiceContext{stableIdentity: &stableIdentityRuntime{}}
	next.stableIdentity.running.Store(true)
	require.Same(t, next, registry.getOrInitialize("orders", false, "", func(int) *ServiceContext { return next }))
	require.NoError(t, registry.checkAdmission())
}

// TestStableEventSubscriptionAdmission 验证受管理订阅在失去准入时返回错误，恢复后再执行业务。
func TestStableEventSubscriptionAdmission(t *testing.T) {
	stream := event.NewStream()
	bridge := event.NewServiceEventBridge(stream, event.ServiceEventBridgeOptions{})
	defer bridge.Close(context.Background())
	runtime := &stableIdentityRuntime{external: true, ttl: time.Second}
	runtime.running.Store(true)
	sc := &ServiceContext{Service: &types.Service{Name: "orders"}, ServiceEventBridge: bridge, stableIdentity: runtime}
	calls := 0
	cancel, err := sc.SubscribeEvent(event.Subscription{Subject: "orders", EventType: "changed", Reliable: true, Handler: func(context.Context, *event.Envelope) error { calls++; return nil }})
	require.NoError(t, err)
	defer cancel()
	env := event.NewEnvelope("orders", "changed", nil)
	env.Subject = "orders"
	require.Error(t, stream.PublishControl(context.Background(), env))
	require.Zero(t, calls)
	runtime.observe(time.Now(), nil)
	require.NoError(t, stream.PublishControl(context.Background(), env))
	require.Equal(t, 1, calls)
	runtime.beginStop()
	require.Error(t, stream.PublishControl(context.Background(), env))
	require.Equal(t, 1, calls)
}
