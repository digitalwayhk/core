package router_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lifecycleProvider struct {
	registerCount        atomic.Int32
	deregisterCount      atomic.Int32
	registerEntered      chan struct{}
	releaseRegister      chan struct{}
	registerOnce         sync.Once
	registerErr          error
	deregisterErr        error
	registerContextAware bool
}

type lifecycleManagedResource struct {
	closed atomic.Int32
	err    error
}

func (r *lifecycleManagedResource) Close(context.Context) error {
	r.closed.Add(1)
	return r.err
}

func TestServiceContextShutdownClosesManagedResources(t *testing.T) {
	sc := newLifecycleServiceContext("sctest-managed-resource-shutdown")
	wantErr := errors.New("managed resource close failed")
	resource := &lifecycleManagedResource{err: wantErr}
	require.NoError(t, sc.UseResource("orders", resource))

	sc.SetRunState(false)
	sc.SetRunState(false)

	require.Equal(t, int32(1), resource.closed.Load())
	require.ErrorIs(t, sc.ShutdownError(), wantErr)
}

func (p *lifecycleProvider) Name() string { return "lifecycle-test" }

func (p *lifecycleProvider) Register(ctx context.Context, _ *cluster.NodeInfo) error {
	p.registerCount.Add(1)
	if p.registerEntered != nil {
		p.registerOnce.Do(func() { close(p.registerEntered) })
	}
	if p.releaseRegister != nil {
		if p.registerContextAware {
			select {
			case <-p.releaseRegister:
			case <-ctx.Done():
				return ctx.Err()
			}
		} else {
			<-p.releaseRegister
		}
	}
	return p.registerErr
}

func TestServiceContext_RequiredClusterRegistrationFailureStopsStartup(t *testing.T) {
	provider := &lifecycleProvider{registerErr: errors.New("redis unavailable")}
	sc := router.NewServiceContext(&fakeService{name: "sctest-required-cluster-register"})
	sc.Config.Cluster.Mode = "on"
	sc.ClusterProvider = provider

	assert.Panics(t, func() { sc.SetRunState(true) })
	assert.False(t, sc.IsRun())
	assert.Equal(t, int32(1), provider.registerCount.Load())
}

func (p *lifecycleProvider) Deregister(context.Context, string) error {
	p.deregisterCount.Add(1)
	return p.deregisterErr
}

func TestServiceContextRegistrationUsesBoundedContext(t *testing.T) {
	registerEntered := make(chan struct{})
	provider := &lifecycleProvider{registerEntered: registerEntered, registerContextAware: true}
	provider.releaseRegister = make(chan struct{})
	sc := newLifecycleServiceContext("sctest-bounded-register")
	sc.ClusterProvider = provider
	sc.SetLifecycleTimeout(20 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		sc.SetRunState(true)
		close(done)
	}()
	<-registerEntered
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("阻塞 Register 未受生命周期超时控制")
	}
	close(provider.releaseRegister)
	sc.SetRunState(false)
}

func TestServiceContextPreservesDeregisterFailure(t *testing.T) {
	wantErr := errors.New("deregister failed")
	provider := &lifecycleProvider{deregisterErr: wantErr}
	sc := newLifecycleServiceContext("sctest-deregister-error")
	sc.ClusterProvider = provider
	sc.SetLifecycleTimeout(200 * time.Millisecond)
	sc.SetRunState(true)

	sc.SetRunState(false)
	sc.SetRunState(false)

	require.ErrorIs(t, sc.ShutdownError(), wantErr)
	assert.Equal(t, int32(3), provider.deregisterCount.Load())
	assert.Nil(t, router.GetContext(sc.Service.Name), "注销失败也必须移除本地 terminated 实例")
}

func (p *lifecycleProvider) Heartbeat(context.Context, string) error { return nil }

func (p *lifecycleProvider) Get(context.Context, string) (*cluster.NodeInfo, error) {
	return nil, cluster.ErrNodeNotFound
}

func (p *lifecycleProvider) List(context.Context, string, ...cluster.NodeStatus) ([]*cluster.NodeInfo, error) {
	return nil, nil
}

func (p *lifecycleProvider) Watch(context.Context, string, func([]*cluster.NodeInfo)) (func(), error) {
	return func() {}, nil
}

func (p *lifecycleProvider) Close() error { return nil }

func TestServiceContext_ConcurrentStartRegistersOnce(t *testing.T) {
	provider := &lifecycleProvider{
		registerEntered: make(chan struct{}),
		releaseRegister: make(chan struct{}),
	}
	sc := router.NewServiceContext(&fakeService{name: "sctest-concurrent-lifecycle-start"})
	sc.ClusterProvider = provider

	var starts sync.WaitGroup
	starts.Add(1)
	go func() {
		defer starts.Done()
		sc.SetRunState(true)
	}()

	select {
	case <-provider.registerEntered:
	case <-time.After(time.Second):
		t.Fatal("未等到第一次节点注册")
	}
	for range 15 {
		starts.Add(1)
		go func() {
			defer starts.Done()
			sc.SetRunState(true)
		}()
	}
	time.Sleep(30 * time.Millisecond)
	close(provider.releaseRegister)
	starts.Wait()
	t.Cleanup(func() { sc.SetRunState(false) })

	assert.Equal(t, int32(1), provider.registerCount.Load(), "并发启动只能注册一次")
}

func TestServiceContext_RepeatedStateDoesNotNotifyAgain(t *testing.T) {
	provider := &lifecycleProvider{}
	sc := router.NewServiceContext(&fakeService{name: "sctest-idempotent-lifecycle-state"})
	sc.ClusterProvider = provider

	sc.SetRunState(true)
	require.Equal(t, true, <-sc.StateChan)
	sc.SetRunState(true)
	assertNoLifecycleState(t, sc.StateChan)
	assert.Equal(t, int32(1), provider.registerCount.Load())

	sc.SetRunState(false)
	require.Equal(t, false, <-sc.StateChan)
	sc.SetRunState(false)
	assertNoLifecycleState(t, sc.StateChan)
	assert.Equal(t, int32(1), provider.deregisterCount.Load())
}

func assertNoLifecycleState(t *testing.T, states <-chan bool) {
	t.Helper()
	select {
	case state := <-states:
		t.Fatalf("收到了重复的生命周期通知: %t", state)
	case <-time.After(30 * time.Millisecond):
	}
}
