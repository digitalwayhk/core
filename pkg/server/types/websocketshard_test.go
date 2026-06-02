package types

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

type shardCapture struct {
	mu   sync.Mutex
	subs []shardSubscription
}

type shardSubscription struct {
	hash   uint64
	active bool
}

func (c *shardCapture) ForwardNotice(context.Context, string, uint64, interface{}) {}
func (c *shardCapture) OnSubscriptionChange(_ string, hash uint64, active bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs = append(c.subs, shardSubscription{hash: hash, active: active})
}
func (c *shardCapture) DrainAndStop(context.Context) {}

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

func newShardRouterInfo(path string) *RouterInfo {
	return &RouterInfo{Path: path, ServiceName: "svc", PathType: PublicType}
}

func newShardRouter(info *RouterInfo, hash uint64) *shardTestRouter {
	router := &shardTestRouter{info: info, hash: hash}
	info.SetInstance(router)
	return router
}

func TestUnRegisterWebSocketHash_DoubleUnregisterFiresOnce(t *testing.T) {
	capture := &shardCapture{}
	SetCrossNodeForwarder(capture)
	defer SetCrossNodeForwarder(nil)

	info := newShardRouterInfo("/ws/order")
	router := newShardRouter(info, 55)
	ws := &shardTestWebSocket{id: 1}
	req := &shardTestRequest{}

	hash := info.RegisterWebSocketClient(router, ws, req)
	waitForShard(t, func() bool { return len(capture.subs) >= 1 })

	info.UnRegisterWebSocketHash(hash, ws)
	waitForShard(t, func() bool { return capture.inactiveCount(hash) == 1 })

	info.UnRegisterWebSocketHash(hash, ws)
	time.Sleep(50 * time.Millisecond)

	if got := atomic.LoadInt32(&router.unregs); got != 1 {
		t.Fatalf("expected unregister hook once, got %d", got)
	}
	if capture.inactiveCount(hash) != 1 {
		t.Fatalf("expected one inactive event, got %d", capture.inactiveCount(hash))
	}
	if _, ok := info.rHashClients[hash]; ok {
		t.Fatalf("expected hash %d to be removed from rHashClients", hash)
	}
}

func TestUnRegisterWebSocketHash_UnknownClientDoesNotChangeCount(t *testing.T) {
	capture := &shardCapture{}
	SetCrossNodeForwarder(capture)
	defer SetCrossNodeForwarder(nil)

	info := newShardRouterInfo("/ws/price")
	router := newShardRouter(info, 66)
	registered := &shardTestWebSocket{id: 1}
	unknown := &shardTestWebSocket{id: 2}
	req := &shardTestRequest{}

	hash := info.RegisterWebSocketClient(router, registered, req)
	waitForShard(t, func() bool { return len(capture.subs) >= 1 })

	info.UnRegisterWebSocketHash(hash, unknown)
	time.Sleep(50 * time.Millisecond)

	if got := info.rHashClients[hash]; got != 1 {
		t.Fatalf("expected hash count to remain 1, got %d", got)
	}
	if got := atomic.LoadInt32(&router.unregs); got != 0 {
		t.Fatalf("expected unregister hook to stay at 0, got %d", got)
	}
	if capture.inactiveCount(hash) != 0 {
		t.Fatalf("expected no inactive events, got %d", capture.inactiveCount(hash))
	}
}
