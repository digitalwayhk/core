package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func (s *autoFallbackService) ServiceName() string    { return s.name }
func (*autoFallbackService) Routers() []types.IRouter { return nil }

type autoFallbackProvider struct {
	name             string
	registerErr      error
	deregisterErr    error
	registerCount    atomic.Int32
	deregisterCount  atomic.Int32
	recordOnRegister bool
	registered       atomic.Bool
}

func (p *autoFallbackProvider) Name() string { return p.name }
func (p *autoFallbackProvider) Register(context.Context, *cluster.NodeInfo) error {
	p.registerCount.Add(1)
	if p.recordOnRegister || p.registerErr == nil {
		p.registered.Store(true)
	}
	return p.registerErr
}
func (p *autoFallbackProvider) Deregister(context.Context, string) error {
	p.deregisterCount.Add(1)
	if p.deregisterErr == nil || errors.Is(p.deregisterErr, cluster.ErrNodeNotFound) {
		p.registered.Store(false)
	}
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

func TestServiceContextAutoFallbackCleansPartiallyRegisteredProvider(t *testing.T) {
	external := &autoFallbackProvider{
		name: "redis", registerErr: errors.New("register response lost"), recordOnRegister: true,
	}
	local := &autoFallbackProvider{name: "local"}
	sc := newAutoFallbackContext(t, "partial-register-cleanup")
	sc.ClusterProvider = external
	sc.localFallbackProvider = local
	sc.ServiceResolver.SetProvider(external)

	sc.SetRunState(true)
	require.True(t, sc.IsRun())
	assert.False(t, external.registered.Load(), "切换本地前必须清理外部 provider 的半成功记录")
	assert.Equal(t, int32(1), external.deregisterCount.Load())
	assert.Equal(t, int32(1), local.registerCount.Load())
	require.Same(t, local, sc.ClusterProvider)
	sc.SetRunState(false)
}

func TestServiceContextAutoFallbackContinuesAfterSecondCleanupSucceeds(t *testing.T) {
	external := &autoFallbackProvider{
		name: "consul",
		registerErr: &cluster.RegistrationError{
			Cause:       errors.New("registration compensation failed"),
			Compensated: false,
		},
		recordOnRegister: true,
	}
	local := &autoFallbackProvider{name: "local"}
	sc := newAutoFallbackContext(t, "second-cleanup-success")
	sc.ClusterProvider = external
	sc.localFallbackProvider = local
	sc.ServiceResolver.SetProvider(external)

	sc.SetRunState(true)
	require.True(t, sc.IsRun())
	assert.False(t, external.registered.Load())
	assert.Equal(t, int32(1), external.deregisterCount.Load())
	assert.Equal(t, int32(1), local.registerCount.Load())
	require.Same(t, local, sc.ClusterProvider)
	sc.SetRunState(false)
}

func TestServiceContextAutoFallbackAcceptsSecondCleanupNodeNotFound(t *testing.T) {
	external := &autoFallbackProvider{
		name: "consul",
		registerErr: &cluster.RegistrationError{
			Cause:       errors.New("registration compensation result lost"),
			Compensated: false,
		},
		deregisterErr:    cluster.ErrNodeNotFound,
		recordOnRegister: true,
	}
	local := &autoFallbackProvider{name: "local"}
	sc := newAutoFallbackContext(t, "second-cleanup-not-found")
	sc.ClusterProvider = external
	sc.localFallbackProvider = local
	sc.ServiceResolver.SetProvider(external)

	sc.SetRunState(true)
	require.True(t, sc.IsRun())
	assert.Equal(t, int32(1), external.deregisterCount.Load())
	assert.Equal(t, int32(1), local.registerCount.Load())
	require.Same(t, local, sc.ClusterProvider)
	sc.SetRunState(false)
}

func TestServiceContextAutoFallbackUsesConsulCompensationWithoutDoubleDeregister(t *testing.T) {
	consulServer, deregisterCount := newFailingTTLConsulServer(t, false)
	provider, err := cluster.NewConsulProvider(consulServer.URL)
	require.NoError(t, err)
	local := &autoFallbackProvider{name: "local"}
	sc := newAutoFallbackContext(t, "consul-compensated")
	sc.ClusterProvider = provider
	sc.localFallbackProvider = local
	sc.ServiceResolver.SetProvider(provider)

	sc.SetRunState(true)
	require.True(t, sc.IsRun())
	assert.Equal(t, int32(1), deregisterCount.Load(), "Consul 已补偿成功时不得二次注销")
	assert.Equal(t, int32(1), local.registerCount.Load())
	require.Same(t, local, sc.ClusterProvider)
	sc.SetRunState(false)
}

func TestServiceContextAutoFallbackRejectsConsulWhenCompensationFails(t *testing.T) {
	consulServer, deregisterCount := newFailingTTLConsulServer(t, true)
	provider, err := cluster.NewConsulProvider(consulServer.URL)
	require.NoError(t, err)
	local := &autoFallbackProvider{name: "local"}
	sc := newAutoFallbackContext(t, "consul-compensation-failed")
	sc.ClusterProvider = provider
	sc.localFallbackProvider = local
	sc.ServiceResolver.SetProvider(provider)

	sc.SetRunState(true)
	require.False(t, sc.IsRun())
	assert.Equal(t, int32(2), deregisterCount.Load(), "补偿失败应再次尝试清理，但仍须 fail closed")
	assert.Zero(t, local.registerCount.Load(), "补偿失败不得 fallback")
	require.Error(t, sc.RuntimeError())
}

func newFailingTTLConsulServer(t *testing.T, failDeregister bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var registered atomic.Bool
	var deregisterCount atomic.Int32
	var serviceMu sync.RWMutex
	var service map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/health/service/"):
			w.Header().Set("Content-Type", "application/json")
			if !registered.Load() {
				_, _ = fmt.Fprint(w, "[]")
				return
			}
			serviceMu.RLock()
			current := service
			serviceMu.RUnlock()
			require.NoError(t, json.NewEncoder(w).Encode([]map[string]interface{}{{
				"Service": current,
				"Checks":  []interface{}{},
			}}))
		case r.URL.Path == "/v1/agent/service/register":
			var registration map[string]interface{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&registration))
			serviceMu.Lock()
			service = registration
			serviceMu.Unlock()
			registered.Store(true)
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/v1/agent/check/update/"):
			http.Error(w, "ttl failed", http.StatusInternalServerError)
		case strings.HasPrefix(r.URL.Path, "/v1/agent/service/deregister/"):
			deregisterCount.Add(1)
			if failDeregister {
				http.Error(w, "cleanup failed", http.StatusInternalServerError)
				return
			}
			registered.Store(false)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &deregisterCount
}

func TestServiceContextAutoFallbackFailsClosedWhenPartialRegistrationCleanupFails(t *testing.T) {
	registerErr := errors.New("register response lost")
	cleanupErr := errors.New("cleanup unavailable")
	external := &autoFallbackProvider{
		name: "redis", registerErr: registerErr, deregisterErr: cleanupErr, recordOnRegister: true,
	}
	local := &autoFallbackProvider{name: "local"}
	sc := newAutoFallbackContext(t, "partial-register-cleanup-failure")
	sc.ClusterProvider = external
	sc.localFallbackProvider = local
	sc.ServiceResolver.SetProvider(external)

	sc.SetRunState(true)
	require.False(t, sc.IsRun())
	require.ErrorIs(t, sc.RuntimeError(), registerErr)
	require.ErrorIs(t, sc.RuntimeError(), cleanupErr)
	assert.True(t, external.registered.Load(), "清理失败时应保留外部残留事实供诊断")
	assert.Equal(t, int32(1), external.deregisterCount.Load())
	assert.Zero(t, local.registerCount.Load(), "清理失败不得继续注册本地 provider")
	assert.Same(t, external, sc.ClusterProvider)
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
