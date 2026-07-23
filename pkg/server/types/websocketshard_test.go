package types

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
)

type shardTestWebSocket struct{ id int }

func (s *shardTestWebSocket) Send(string, string, interface{}) {}
func (s *shardTestWebSocket) IsClosed() bool                   { return false }

type shardTestResponse struct{}

func (s *shardTestResponse) GetSuccess() bool                   { return true }
func (s *shardTestResponse) GetMessage() string                 { return "" }
func (s *shardTestResponse) GetData(...interface{}) interface{} { return nil }
func (s *shardTestResponse) GetError() error                    { return nil }

type shardTestRequest struct{}

func (s *shardTestRequest) GetTraceId() string        { return "trace" }
func (s *shardTestRequest) GetUser() (string, string) { return "", "" }
func (s *shardTestRequest) GetClientIP() string       { return "127.0.0.1" }
func (s *shardTestRequest) NewID() uint               { return 1 }
func (s *shardTestRequest) Authorized() bool          { return true }
func (s *shardTestRequest) CallService(IRouter, ...func(IResponse)) (IResponse, error) {
	return &shardTestResponse{}, nil
}
func (s *shardTestRequest) CallTargetService(IRouter, *TargetInfo, ...func(IResponse)) (IResponse, error) {
	return &shardTestResponse{}, nil
}
func (s *shardTestRequest) GetValue(string) string                   { return "" }
func (s *shardTestRequest) Bind(interface{}) error                   { return nil }
func (s *shardTestRequest) GoZeroBind(interface{}) error             { return nil }
func (s *shardTestRequest) NewResponse(interface{}, error) IResponse { return &shardTestResponse{} }
func (s *shardTestRequest) GetPath() string                          { return "" }
func (s *shardTestRequest) GetClaims(string) interface{}             { return nil }
func (s *shardTestRequest) ServiceName() string                      { return "svc" }
func (s *shardTestRequest) GetServerInfo() *TargetInfo               { return nil }
func (s *shardTestRequest) GetTargetServerInfo(string) *TargetInfo   { return nil }

type shardTestRouter struct {
	info   *RouterInfo
	hash   uint64
	unregs int32
}

func (s *shardTestRouter) Parse(IRequest) error                   { return nil }
func (s *shardTestRouter) Validation(IRequest) error              { return nil }
func (s *shardTestRouter) Do(IRequest) (interface{}, error)       { return nil, nil }
func (s *shardTestRouter) RouterInfo() *RouterInfo                { return s.info }
func (s *shardTestRouter) RegisterWebSocket(IWebSocket, IRequest) {}
func (s *shardTestRouter) UnRegisterWebSocket(IWebSocket, IRequest) {
	atomic.AddInt32(&s.unregs, 1)
}
func (s *shardTestRouter) GetHashKey() uint64 { return s.hash }
func (s *shardTestRouter) NoticeFiltersRouter(message interface{}, _ IRouter) (bool, interface{}) {
	return true, message
}

type shardCapture struct {
	mu      sync.Mutex
	subs    []shardSubscription
	notices []uint64
}

type shardSubscription struct {
	hash   uint64
	active bool
}

func (c *shardCapture) ForwardNotice(_ context.Context, _ string, hash uint64, _ interface{}) {
	c.mu.Lock()
	c.notices = append(c.notices, hash)
	c.mu.Unlock()
}
func (c *shardCapture) OnSubscriptionChange(_ string, hash uint64, active bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs = append(c.subs, shardSubscription{hash: hash, active: active})
}
func (c *shardCapture) DrainAndStop(context.Context) {}

func (c *shardCapture) subscriptionCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.subs)
}

func (c *shardCapture) inactiveCount(hash uint64) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, sub := range c.subs {
		if sub.hash == hash && !sub.active {
			count++
		}
	}
	return count
}

func (c *shardCapture) noticeCount(hash uint64) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, noticeHash := range c.notices {
		if noticeHash == hash {
			count++
		}
	}
	return count
}

func waitForShard(t *testing.T, check func() bool) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func newShardRouterInfo(t *testing.T, path string) *RouterInfo {
	return newShardRouterInfoForService(t, "svc", path)
}

func newShardRouterInfoForService(t *testing.T, serviceName, path string) *RouterInfo {
	t.Helper()
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})
	hub := NewRouteWebSocketHub(serviceName, bridge)
	info := &RouterInfo{Path: path, ServiceName: serviceName, PathType: PublicType}
	info.SetWebSocketHub(serviceName, hub)
	t.Cleanup(func() {
		if err := hub.Close(context.Background()); err != nil {
			t.Errorf("关闭 RouteWebSocketHub: %v", err)
		}
		if err := bridge.Close(context.Background()); err != nil {
			t.Errorf("关闭 ServiceEventBridge: %v", err)
		}
	})
	return info
}

func newShardRouter(info *RouterInfo, hash uint64) *shardTestRouter {
	router := &shardTestRouter{info: info, hash: hash}
	info.SetInstance(router)
	return router
}

func TestUnRegisterWebSocketHash_DoubleUnregisterFiresOnce(t *testing.T) {
	capture := &shardCapture{}
	SetCrossNodeForwarderForService("svc", capture)
	defer ClearCrossNodeForwarderForService("svc", capture)

	info := newShardRouterInfo(t, "/ws/order")
	router := newShardRouter(info, 55)
	ws := &shardTestWebSocket{id: 1}
	req := &shardTestRequest{}

	hash := info.RegisterWebSocketClient(router, ws, req)
	waitForShard(t, func() bool { return capture.subscriptionCount() >= 1 })

	info.UnRegisterWebSocketHash(hash, ws)
	waitForShard(t, func() bool { return capture.inactiveCount(hash) == 1 })

	info.UnRegisterWebSocketHash(hash, ws)

	if got := atomic.LoadInt32(&router.unregs); got != 1 {
		t.Fatalf("expected unregister hook once, got %d", got)
	}
	if capture.inactiveCount(hash) != 1 {
		t.Fatalf("expected one inactive event, got %d", capture.inactiveCount(hash))
	}
	if _, ok := info.webSocketHub.hashClientCounts(info)[hash]; ok {
		t.Fatalf("expected hash %d to be removed from RouteWebSocketHub", hash)
	}
}

func TestUnRegisterWebSocketHash_UnknownClientDoesNotChangeCount(t *testing.T) {
	capture := &shardCapture{}
	SetCrossNodeForwarderForService("svc", capture)
	defer ClearCrossNodeForwarderForService("svc", capture)

	info := newShardRouterInfo(t, "/ws/price")
	router := newShardRouter(info, 66)
	registered := &shardTestWebSocket{id: 1}
	unknown := &shardTestWebSocket{id: 2}
	req := &shardTestRequest{}

	hash := info.RegisterWebSocketClient(router, registered, req)
	waitForShard(t, func() bool { return capture.subscriptionCount() >= 1 })

	info.UnRegisterWebSocketHash(hash, unknown)

	if got := info.webSocketHub.hashClientCounts(info)[hash]; got != 1 {
		t.Fatalf("expected hash count to remain 1, got %d", got)
	}
	if got := atomic.LoadInt32(&router.unregs); got != 0 {
		t.Fatalf("expected unregister hook to stay at 0, got %d", got)
	}
	if capture.inactiveCount(hash) != 0 {
		t.Fatalf("expected no inactive events, got %d", capture.inactiveCount(hash))
	}
}

func TestWebSocketForwardersAreIsolatedByService(t *testing.T) {
	serviceA := "ws-service-a"
	serviceB := "ws-service-b"
	forwarderA := &shardCapture{}
	forwarderB := &shardCapture{}
	SetCrossNodeForwarderForService(serviceA, forwarderA)
	SetCrossNodeForwarderForService(serviceB, forwarderB)
	t.Cleanup(func() {
		ClearCrossNodeForwarderForService(serviceA, forwarderA)
		ClearCrossNodeForwarderForService(serviceB, forwarderB)
	})

	infoA := newShardRouterInfoForService(t, serviceA, "/ws/service-a")
	routerA := newShardRouter(infoA, 101)
	hashA := infoA.RegisterWebSocketClient(routerA, &shardTestWebSocket{id: 101}, &shardTestRequest{})
	infoB := newShardRouterInfoForService(t, serviceB, "/ws/service-b")
	routerB := newShardRouter(infoB, 202)
	hashB := infoB.RegisterWebSocketClient(routerB, &shardTestWebSocket{id: 202}, &shardTestRequest{})

	waitForShard(t, func() bool {
		return forwarderA.subscriptionCount() == 1 && forwarderB.subscriptionCount() == 1
	})
	infoA.NoticeWebSocket("service-a")
	waitForShard(t, func() bool { return forwarderA.noticeCount(hashA) == 1 })
	if got := forwarderB.noticeCount(hashA); got != 0 {
		t.Fatalf("服务 A 的通知被转发到服务 B：%d", got)
	}

	infoB.NoticeWebSocket("service-b")
	waitForShard(t, func() bool { return forwarderB.noticeCount(hashB) == 1 })
	if got := forwarderA.noticeCount(hashB); got != 0 {
		t.Fatalf("服务 B 的通知被转发到服务 A：%d", got)
	}
}
