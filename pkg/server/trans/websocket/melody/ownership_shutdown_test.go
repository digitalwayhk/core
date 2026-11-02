package melody

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	ws "github.com/olahol/melody"
	"github.com/stretchr/testify/require"
)

type shutdownGatewayService struct{ name string }

func (s *shutdownGatewayService) ServiceName() string    { return s.name }
func (*shutdownGatewayService) Routers() []types.IRouter { return nil }

type shutdownGatewayResource struct {
	manager  *MelodyManager
	admitted <-chan struct{}
}

func (r shutdownGatewayResource) Close(ctx context.Context) error {
	// 模拟已接纳的订阅刷新：真实退出必须等待它，再停止原生Gateway worker。
	<-r.admitted
	return r.manager.CloseContext(ctx)
}

// 正式ServiceContext、真实Redis Store与原生Gateway心跳共享退出生命周期。
func TestGatewayReferenceFlushConcurrentServiceShutdown(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: 需要独占CORE_TEST_REDIS_ADDR")
	}
	for round := 0; round < 4; round++ {
		name := fmt.Sprintf("gateway-shutdown-%d-%d", time.Now().UnixNano(), round)
		cfg := config.NewServiceDefaultConfig(name, 29980+round)
		cfg.Cluster.Mode = "off"
		cfg.MQ.Mode = "on"
		cfg.MQ.Provider = "redis-stream"
		cfg.MQ.RedisStream = config.RedisStreamMQConfig{Addr: addr, Prefix: name, CommandPoolSize: 4, BlockingReadPoolSize: 4}
		sc := router.NewServiceContextWithConfig(&shutdownGatewayService{name}, cfg)
		m := ownership.Manifest{Scope: "stats", Service: name, Group: name, Selector: "v1", Keys: []string{"A"}, Subjects: []string{"input"}}.Normalize()
		require.NoError(t, sc.MQManager.ProvisionOwnership(context.Background(), m, 0))
		require.NoError(t, sc.MQManager.BindOwnership(context.Background(), m))
		sc.SetRunState(true)
		manager := NewMelodyManager(sc)
		session := NewSessionSubscriptions(manager, nil, nil)
		session.subscriptions = map[string]map[uint64]types.IRouter{"/view": {1: &ownedReferenceTestRouter{Key: "A"}}}
		manager.subscriptions[&ws.Session{}] = session
		require.NoError(t, manager.ensureOwnershipReferences(context.Background(), ownership.ScopedReference{Scope: "stats", Key: "A", Name: "view"}))
		ready, done := make(chan struct{}), make(chan struct{})
		require.NoError(t, sc.UseResource("websocket-melody", shutdownGatewayResource{manager, done}))
		go func() {
			defer close(done)
			close(ready)
			for i := 0; i < 64; i++ {
				_ = manager.ensureOwnershipReferences(context.Background(), ownership.ScopedReference{Scope: "stats", Key: "A", Name: "view"})
				_ = manager.flushOwnershipReferences()
			}
		}()
		<-ready
		sc.SetRunState(false)
		require.NoError(t, sc.ShutdownError())
		select {
		case <-manager.ownershipReferences.done:
		default:
			t.Fatal("ServiceContext退出未等待原生引用worker")
		}
	}
}

// 仅在真实Redis提交之后拦截返回，观察关闭是否取消并等待在途刷新。
type shutdownBarrierStore struct {
	ownership.Store
	entered, returned chan struct{}
	writes            atomic.Int32
	closedAfterReturn atomic.Bool
}

func (s *shutdownBarrierStore) ReplaceGateway(ctx context.Context, snapshot ownership.GatewaySnapshot) error {
	s.writes.Add(1)
	if err := s.Store.ReplaceGateway(ctx, snapshot); err != nil {
		return err
	}
	close(s.entered)
	<-ctx.Done()
	close(s.returned)
	return ctx.Err()
}
func (s *shutdownBarrierStore) CloseGateway(ctx context.Context, id string) error {
	select {
	case <-s.returned:
		s.closedAfterReturn.Store(true)
	default:
	}
	return s.Store.CloseGateway(ctx, id)
}

func TestGatewayCloseCancelsFlushBeforeClosingReferenceLease(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: 需要独占CORE_TEST_REDIS_ADDR")
	}
	provider := mq.NewRedisStreamProvider(addr, fmt.Sprintf("gateway-cancel-%d", time.Now().UnixNano()), 0)
	require.NoError(t, provider.Connect(context.Background()))
	defer provider.Close()
	store, err := provider.OwnershipStore(context.Background())
	require.NoError(t, err)
	barrier := &shutdownBarrierStore{Store: store, entered: make(chan struct{}), returned: make(chan struct{})}
	manager := NewMelodyManager(nil)
	runtime, err := newGatewayReferenceRuntime(manager, barrier)
	require.NoError(t, err)
	manager.ownershipMu.Lock()
	manager.ownershipReferences = runtime
	manager.ownershipMu.Unlock()
	runtime.wake <- struct{}{}
	select {
	case <-barrier.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("未进入真实Redis快照提交后的刷新")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, manager.CloseContext(ctx), "关闭应取消在途刷新，不能等待原3秒超时")
	require.True(t, barrier.closedAfterReturn.Load(), "必须等实际刷新返回才能关闭Gateway lease")
	before := barrier.writes.Load()
	require.ErrorIs(t, manager.flushOwnershipReferences(), context.Canceled)
	require.Equal(t, before, barrier.writes.Load(), "已关闭心跳不能再次提交或复活引用")
}

type gatewayCancellationOrderStore struct {
	ownership.Store
	runtimeContext func() error
	observed       chan error
}

func (s *gatewayCancellationOrderStore) CloseGateway(ctx context.Context, id string) error {
	s.observed <- s.runtimeContext()
	return s.Store.CloseGateway(ctx, id)
}

// 已接纳握手延迟finishClose时，原worker也必须先取消，再关闭lease和done。
func TestGatewayWorkerCancelsBeforeLeaseCleanupWhileAdmissionDrains(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: 需要独占CORE_TEST_REDIS_ADDR")
	}
	provider := mq.NewRedisStreamProvider(addr, fmt.Sprintf("gateway-order-%d", time.Now().UnixNano()), 0)
	require.NoError(t, provider.Connect(context.Background()))
	defer provider.Close()
	store, err := provider.OwnershipStore(context.Background())
	require.NoError(t, err)
	probe := &gatewayCancellationOrderStore{Store: store, observed: make(chan error, 1)}
	manager := NewMelodyManager(nil)
	runtime, err := newGatewayReferenceRuntime(manager, probe)
	require.NoError(t, err)
	probe.runtimeContext = runtime.ctx.Err
	manager.ownershipMu.Lock()
	manager.ownershipReferences = runtime
	manager.ownershipMu.Unlock()
	manager.admissions.Add(1)
	closed := make(chan error, 1)
	go func() { closed <- manager.Close() }()
	defer func() { manager.admissions.Done(); require.NoError(t, <-closed) }()
	select {
	case state := <-probe.observed:
		require.ErrorIs(t, state, context.Canceled, "lease清理前必须先取消运行时，不能留下再次开户窗口")
	case <-time.After(2 * time.Second):
		t.Fatal("原worker未响应关闭")
	}
}
