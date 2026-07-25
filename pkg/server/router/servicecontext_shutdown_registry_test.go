package router

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type shutdownRegistryService struct{ name string }

func (s *shutdownRegistryService) ServiceName() string    { return s.name }
func (*shutdownRegistryService) Routers() []types.IRouter { return nil }

type shutdownBlockingProvider struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (*shutdownBlockingProvider) Name() string                  { return "shutdown-blocking" }
func (*shutdownBlockingProvider) Connect(context.Context) error { return nil }
func (*shutdownBlockingProvider) Health(context.Context) error  { return nil }
func (*shutdownBlockingProvider) Publish(context.Context, string, []byte, *mq.PublishOptions) error {
	return nil
}
func (*shutdownBlockingProvider) Subscribe(context.Context, string, func(*mq.Message)) (func(), error) {
	return func() {}, nil
}
func (p *shutdownBlockingProvider) Close() error {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return nil
}

func TestServiceContextNotReusedWhileShuttingDown(t *testing.T) {
	serviceName := fmt.Sprintf("shutdown-registry-%d", time.Now().UnixNano())
	service := &shutdownRegistryService{name: serviceName}
	cfg := config.NewServiceDefaultConfig(serviceName, 31991)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	cfg.Transport.Internal = ""
	cfg.Transport.Fallback = nil
	first := NewServiceContextWithConfig(service, cfg)
	first.SetRunState(true)

	provider := &shutdownBlockingProvider{entered: make(chan struct{}), release: make(chan struct{})}
	manager := mq.NewManager()
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent(provider.Name()))
	first.MQManager = manager

	shutdownDone := make(chan struct{})
	go func() {
		first.SetRunState(false)
		close(shutdownDone)
	}()
	select {
	case <-provider.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("ServiceContext 未进入受控关闭窗口")
	}

	created := make(chan *ServiceContext, 1)
	go func() { created <- NewServiceContextWithConfig(service, cfg) }()

	var early *ServiceContext
	select {
	case early = <-created:
	case <-time.After(100 * time.Millisecond):
	}
	close(provider.release)
	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ServiceContext 关闭未完成")
	}

	if early != nil {
		assert.NotSame(t, first, early, "关闭窗口不得返回 terminated ServiceContext")
		return
	}
	select {
	case second := <-created:
		require.NotSame(t, first, second)
		second.SetRunState(true)
		t.Cleanup(func() { second.SetRunState(false) })
	case <-time.After(2 * time.Second):
		t.Fatal("关闭完成后未创建同名新 ServiceContext")
	}
}

func TestServiceContextRegistryRebuildsOnceForMultipleShutdownWaiters(t *testing.T) {
	registry := newServiceContextRegistry()
	const serviceName = "multi-shutdown-waiters"
	first := &ServiceContext{terminated: true, shutdownDone: make(chan struct{})}
	registry.contexts[serviceName] = first

	const waiters = 32
	results := make(chan *ServiceContext, waiters)
	started := make(chan struct{}, waiters)
	initializeEntered := make(chan struct{})
	releaseInitialize := make(chan struct{})
	var initializations atomic.Int32
	var group sync.WaitGroup
	launch := func() {
		group.Add(1)
		go func() {
			defer group.Done()
			started <- struct{}{}
			results <- registry.getOrInitialize(serviceName, false, "fingerprint", func(int) *ServiceContext {
				if initializations.Add(1) == 1 {
					close(initializeEntered)
				}
				<-releaseInitialize
				return &ServiceContext{configFingerprint: "fingerprint"}
			})
		}()
	}
	launch()
	<-started
	require.True(t, registry.remove(serviceName, first))
	close(first.shutdownDone)
	<-initializeEntered
	for range waiters - 1 {
		launch()
	}
	for range waiters - 1 {
		<-started
	}
	close(releaseInitialize)
	group.Wait()
	close(results)

	assert.Equal(t, int32(1), initializations.Load())
	var rebuilt *ServiceContext
	for result := range results {
		require.NotSame(t, first, result)
		if rebuilt == nil {
			rebuilt = result
			continue
		}
		assert.Same(t, rebuilt, result)
	}
}
