package router_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	runtimeFactoryOnce  sync.Once
	runtimeFactoryCalls atomic.Int32
	runtimeServiceID    atomic.Uint64
)

type runtimeMQProvider struct {
	closeCalls atomic.Int32
}

func (*runtimeMQProvider) Name() string                  { return "redis-stream" }
func (*runtimeMQProvider) Connect(context.Context) error { return nil }
func (p *runtimeMQProvider) Close() error {
	p.closeCalls.Add(1)
	return nil
}
func (*runtimeMQProvider) Publish(context.Context, string, []byte, *mq.PublishOptions) error {
	return nil
}
func (*runtimeMQProvider) Subscribe(context.Context, string, func(*mq.Message)) (func(), error) {
	return func() {}, nil
}
func (*runtimeMQProvider) Health(context.Context) error { return nil }

func installRuntimeMQFactory() {
	runtimeFactoryOnce.Do(func() {
		mq.RegisterProviderFactory("redis-stream", func(ctx context.Context, cfg *config.MQConfig) (mq.MQProvider, error) {
			if cfg.RedisStream.Addr == "task-14.2-fake" {
				runtimeFactoryCalls.Add(1)
				return &runtimeMQProvider{}, nil
			}
			provider := mq.NewRedisStreamProvider(cfg.RedisStream.Addr, cfg.RedisStream.Prefix, cfg.RedisStream.DB)
			if err := provider.Connect(ctx); err != nil {
				return nil, fmt.Errorf("%w: connect redis-stream: %v", mq.ErrProviderUnavailable, err)
			}
			return provider, nil
		})
	})
}

func runtimeConfig(name string) *config.ServerConfig {
	con := config.NewServiceDefaultConfig(name, 29842)
	con.Cluster.Mode = "on"
	con.Cluster.Provider = "local"
	con.MQ.Mode = "on"
	con.MQ.Provider = "redis-stream"
	con.MQ.Usage = []string{"event-stream"}
	con.MQ.RedisStream.Addr = "task-14.2-fake"
	return con
}

func uniqueRuntimeServiceName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, runtimeServiceID.Add(1))
}

func TestServiceContextConfigContract_NilConfigPanicsClearly(t *testing.T) {
	name := uniqueRuntimeServiceName("config-contract-nil")
	assert.PanicsWithValue(t, "config validation failed: config is nil", func() {
		router.NewServiceContextWithConfig(&fakeService{name: name}, nil)
	})
}

func TestServiceContextConfigContract_ValidatesBeforeRuntimeProvider(t *testing.T) {
	installRuntimeMQFactory()
	name := uniqueRuntimeServiceName("config-contract-invalid")
	con := runtimeConfig(name)
	con.TrustedProxies = []string{"not-an-ip"}
	before := runtimeFactoryCalls.Load()

	var panicValue interface{}
	func() {
		defer func() { panicValue = recover() }()
		router.NewServiceContextWithConfig(&fakeService{name: name}, con)
	}()
	require.NotNil(t, panicValue)
	assert.Contains(t, fmt.Sprint(panicValue), "config validation failed")
	assert.Equal(t, before, runtimeFactoryCalls.Load(), "配置校验失败前不得创建 runtime provider")
}

func TestServiceContextConfigContract_AutoUnknownMQProviderPanics(t *testing.T) {
	name := uniqueRuntimeServiceName("config-contract-unknown-mq")
	con := config.NewServiceDefaultConfig(name, 29843)
	con.MQ.Mode = "auto"
	con.MQ.Provider = "task-14.2-unknown"

	panicValue := requirePanicValue(t, func() {
		router.NewServiceContextWithConfig(&fakeService{name: name}, con)
	})
	assert.Contains(t, fmt.Sprint(panicValue), "register a provider factory")
}

func requirePanicValue(t *testing.T, f func()) (panicValue interface{}) {
	t.Helper()
	defer func() {
		panicValue = recover()
		require.NotNil(t, panicValue)
	}()
	f()
	return nil
}

func TestServiceContextRuntimeLifecycle_ProductionConstructorOwnsRuntime(t *testing.T) {
	installRuntimeMQFactory()
	name := uniqueRuntimeServiceName("runtime-lifecycle")
	con := runtimeConfig(name)
	sc := router.NewServiceContextWithConfig(&fakeService{name: name}, con)

	require.NotNil(t, sc.ClusterProvider)
	assert.Equal(t, "local", sc.ClusterProvider.Name())
	require.NotNil(t, sc.TransportSelector)
	require.NotNil(t, sc.MQManager)
	require.NotNil(t, sc.EventStream)
	require.NotNil(t, sc.EventBridge)
	provider, ok := sc.MQManager.Current().(*runtimeMQProvider)
	require.True(t, ok)

	sc.SetRunState(true)
	require.NotNil(t, sc.CrossNodeBroker)
	nodes, err := sc.ClusterProvider.List(context.Background(), name)
	require.NoError(t, err)
	require.Len(t, nodes, 1, "启动后 membership 应注册本服务节点")

	sc.SetRunState(false)
	assert.Nil(t, sc.CrossNodeBroker)
	assert.Nil(t, sc.MQManager)
	assert.Nil(t, sc.EventBridge)
	assert.Nil(t, sc.EventStream)
	assert.Equal(t, int32(1), provider.closeCalls.Load())
	nodes, err = sc.ClusterProvider.List(context.Background(), name, cluster.NodeStatusRunning)
	require.NoError(t, err)
	assert.Empty(t, nodes, "停止后 membership 不应保留 running 节点")
	offlineNodes, err := sc.ClusterProvider.List(context.Background(), name, cluster.NodeStatusOffline)
	require.NoError(t, err)
	require.Len(t, offlineNodes, 1)

	sc.SetRunState(false)
	assert.Equal(t, int32(1), provider.closeCalls.Load(), "重复停止不得重复关闭 MQ provider")
	sc.SetRunState(true)
	assert.Nil(t, sc.MQManager, "终止型生命周期不得在重新置 true 时悄悄重连 MQ")
	sc.SetRunState(false)
}
