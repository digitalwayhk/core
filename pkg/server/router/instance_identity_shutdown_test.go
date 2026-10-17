// 本文件验证事件处理器未停止时不能释放 stable Boot，避免错误批准新进程接管。
package router

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/identity"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestStableShutdownKeepsBootWhenEventDrainTimesOut(t *testing.T) {
	ctx := context.Background()
	a := identity.NewAuthority(identity.NewMemoryStore(), "epoch-a")
	require.NoError(t, a.Initialize(ctx))
	b, err := a.Bind(ctx, "orders", "a", 1, 0, 31, false)
	require.NoError(t, err)
	epoch, err := a.AcquireBoot(ctx, b.Key, "boot-a", "", false)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "identity.json")
	require.NoError(t, identity.Create(path, "orders", "a"))
	f, err := identity.Open(path, "orders", "a")
	require.NoError(t, err)
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	_, err = bridge.SubscribeControl("blocked", func(*event.Envelope) error { close(entered); <-release; return nil })
	require.NoError(t, err)
	go func() {
		defer close(finished)
		_ = bridge.Publish(ctx, event.PublishRequest{Class: event.ControlDelivery, Envelope: event.NewEnvelope("orders", "blocked", nil)})
	}()
	<-entered
	defer func() { close(release); <-finished; require.NoError(t, bridge.Close(ctx)) }()
	r := &stableIdentityRuntime{file: f, authority: a, binding: b, owner: cluster.RegistrationOwner{BootID: "boot-a", Epoch: epoch}}
	sc := &ServiceContext{Config: config.NewServiceDefaultConfig("orders", 18080), Service: &types.Service{Name: "orders"}, ServiceEventBridge: bridge, stableIdentity: r}
	sc.isStart.Store(true)
	r.running.Store(true)
	sc.SetRunState(false)
	require.ErrorIs(t, sc.ShutdownError(), context.DeadlineExceeded)
	require.NoError(t, a.VerifyBoot(ctx, b.Key, "boot-a", epoch), "事件处理器未停止，不得把 Boot 标记为已安全释放")
}
