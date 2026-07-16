package router

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/transport"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type autoFallbackService struct{ name string }

func (s *autoFallbackService) ServiceName() string                  { return s.name }
func (*autoFallbackService) Routers() []types.IRouter               { return nil }
func (*autoFallbackService) SubscribeRouters() []*types.ObserveArgs { return nil }

type autoFallbackProvider struct {
	name            string
	registerErr     error
	deregisterErr   error
	registerCount   atomic.Int32
	deregisterCount atomic.Int32
}

func (p *autoFallbackProvider) Name() string { return p.name }
func (p *autoFallbackProvider) Register(context.Context, *cluster.NodeInfo) error {
	p.registerCount.Add(1)
	return p.registerErr
}
func (p *autoFallbackProvider) Deregister(context.Context, string) error {
	p.deregisterCount.Add(1)
	return p.deregisterErr
}

type fixedProviderSwitcher struct{ current cluster.DiscoveryProvider }

func (s *fixedProviderSwitcher) Current() cluster.DiscoveryProvider                   { return s.current }
func (*fixedProviderSwitcher) Begin(context.Context, cluster.DiscoveryProvider) error { return nil }
func (*fixedProviderSwitcher) Complete(context.Context) error                         { return nil }
func (*fixedProviderSwitcher) Rollback(context.Context) error                         { return nil }
func (*autoFallbackProvider) Heartbeat(context.Context, string) error                 { return nil }
func (*autoFallbackProvider) Get(context.Context, string) (*cluster.NodeInfo, error) {
	return nil, cluster.ErrNodeNotFound
}
func (*autoFallbackProvider) List(context.Context, string, ...cluster.NodeStatus) ([]*cluster.NodeInfo, error) {
	return nil, nil
}
func (*autoFallbackProvider) Watch(context.Context, string, func([]*cluster.NodeInfo)) (func(), error) {
	return func() {}, nil
}
func (*autoFallbackProvider) Close() error { return nil }

type autoFallbackGRPC struct {
	ready    chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func newAutoFallbackGRPC() *autoFallbackGRPC {
	ready := make(chan struct{})
	close(ready)
	return &autoFallbackGRPC{ready: ready, done: make(chan struct{})}
}
func (s *autoFallbackGRPC) Start()                 { <-s.done }
func (s *autoFallbackGRPC) Stop()                  { _ = s.StopContext(context.Background()) }
func (s *autoFallbackGRPC) Ready() <-chan struct{} { return s.ready }
func (s *autoFallbackGRPC) Done() <-chan struct{}  { return s.done }
func (*autoFallbackGRPC) BeginShutdown()           {}
func (s *autoFallbackGRPC) StopContext(context.Context) error {
	s.stopOnce.Do(func() { close(s.done) })
	return nil
}
func (*autoFallbackGRPC) Err() error { return nil }

type autoFallbackTransport struct{ stopped atomic.Bool }

func (*autoFallbackTransport) Select(context.Context, *types.PayLoad, transport.TransportEndpoints) (transport.Selection, error) {
	return transport.Selection{}, errors.New("not used")
}
func (s *autoFallbackTransport) Stop(context.Context) error {
	s.stopped.Store(true)
	return nil
}

func newAutoFallbackContext(t *testing.T, suffix string) *ServiceContext {
	t.Helper()
	name := fmt.Sprintf("auto-fallback-%s-%d", suffix, time.Now().UnixNano())
	cfg := config.NewServiceDefaultConfig(name, 0)
	cfg.Cluster.Mode = "auto"
	cfg.Cluster.Provider = "local"
	cfg.MQ.Mode = "off"
	sc := NewServiceContextWithConfig(&autoFallbackService{name: name}, cfg)
	sc.SetGRPCServer(newAutoFallbackGRPC())
	return sc
}

func TestServiceContextAutoRegisterFailureFallsBackToLocalProvider(t *testing.T) {
	external := &autoFallbackProvider{name: "redis", registerErr: errors.New("redis unavailable")}
	local := cluster.NewLocalProvider(time.Hour, time.Hour, 0)
	local.Start()
	t.Cleanup(func() { _ = local.Close() })
	sc := newAutoFallbackContext(t, "success")
	sc.ClusterProvider = external
	sc.localFallbackProvider = local
	sc.ServiceResolver.SetProvider(external)

	sc.SetRunState(true)
	require.True(t, sc.IsRun())
	require.Same(t, local, sc.ClusterProvider)
	sc.ServiceResolver.mu.RLock()
	resolverProvider := sc.ServiceResolver.provider
	sc.ServiceResolver.mu.RUnlock()
	require.Same(t, local, resolverProvider)
	require.NotNil(t, sc.CrossNodeBroker)
	assert.Nil(t, sc.RuntimeError())
	require.Equal(t, true, <-sc.StateChan)
	nodes, err := local.List(context.Background(), sc.Service.Name, cluster.NodeStatusRunning)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	sc.SetRunState(false)
}

func TestServiceContextAutoFallbackFailureStopsStartup(t *testing.T) {
	external := &autoFallbackProvider{name: "redis", registerErr: errors.New("redis unavailable")}
	local := &autoFallbackProvider{name: "local", registerErr: errors.New("local unavailable")}
	sc := newAutoFallbackContext(t, "failure")
	sc.ClusterProvider = external
	sc.localFallbackProvider = local
	sc.ServiceResolver.SetProvider(external)
	transportSelector := &autoFallbackTransport{}
	sc.TransportSelector = transportSelector

	sc.SetRunState(true)
	require.False(t, sc.IsRun())
	require.Error(t, sc.RuntimeError())
	grpcServer := sc.grpcServer
	select {
	case <-grpcServer.Done():
	default:
		t.Fatal("双重注册失败必须关闭 gRPC listener")
	}
	sc.lifecycleMu.Lock()
	broker := sc.CrossNodeBroker
	sc.lifecycleMu.Unlock()
	assert.Nil(t, broker)
	assert.Equal(t, int32(1), external.registerCount.Load())
	assert.Equal(t, int32(1), local.registerCount.Load())
	assert.True(t, transportSelector.stopped.Load(), "启动失败必须关闭 transport 客户端池")
	select {
	case state := <-sc.StateChan:
		assert.False(t, state, "双重注册失败不得发布运行态")
	default:
	}
	select {
	case err := <-sc.Failure():
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("双重注册失败未发布 Failure")
	}
}

func TestSyncProviderAfterSwitchFailsClosedWhenOldMembershipCannotStop(t *testing.T) {
	wantErr := errors.New("old provider deregister failed")
	oldProvider := &autoFallbackProvider{name: "old", deregisterErr: wantErr}
	newProvider := &autoFallbackProvider{name: "new"}
	sc := newAutoFallbackContext(t, "switch-stop-error")
	sc.ClusterProvider = oldProvider
	sc.ServiceResolver.SetProvider(oldProvider)
	sc.ClusterSwitcher = &fixedProviderSwitcher{current: newProvider}
	sc.SetRunState(true)
	require.True(t, sc.IsRun())

	err := sc.SyncProviderAfterSwitch()
	require.ErrorIs(t, err, wantErr)
	assert.Same(t, oldProvider, sc.ClusterProvider)
	assert.Zero(t, newProvider.registerCount.Load())
	assert.False(t, sc.IsRun())
	require.ErrorIs(t, sc.RuntimeError(), wantErr)

	oldProvider.deregisterErr = nil
	sc.SetRunState(false)
}
