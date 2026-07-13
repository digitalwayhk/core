package router

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type shutdownRegistryService struct{ name string }

func (s *shutdownRegistryService) ServiceName() string                  { return s.name }
func (*shutdownRegistryService) Routers() []types.IRouter               { return nil }
func (*shutdownRegistryService) SubscribeRouters() []*types.ObserveArgs { return nil }

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
