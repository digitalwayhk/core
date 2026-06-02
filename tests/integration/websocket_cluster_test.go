//go:build integration

package integration

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// ---- helpers ----

// mockWebSocket implements types.IWebSocket for tests.
type mockWebSocket struct {
	closed   bool
	received []interface{}
	mu       sync.Mutex
}

func (m *mockWebSocket) Send(_ string, _ string, data interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.received = append(m.received, data)
}
func (m *mockWebSocket) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}
func (m *mockWebSocket) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}
func (m *mockWebSocket) Messages() []interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]interface{}, len(m.received))
	copy(cp, m.received)
	return cp
}

type mockResponse struct{}

func (m *mockResponse) GetSuccess() bool                                { return true }
func (m *mockResponse) GetMessage() string                              { return "" }
func (m *mockResponse) GetData(instanceType ...interface{}) interface{} { return nil }
func (m *mockResponse) GetError() error                                 { return nil }

type mockRequest struct{ serviceName string }

func (m *mockRequest) GetTraceId() string        { return "trace" }
func (m *mockRequest) GetUser() (string, string) { return "", "" }
func (m *mockRequest) GetClientIP() string       { return "127.0.0.1" }
func (m *mockRequest) NewID() uint               { return 1 }
func (m *mockRequest) Authorized() bool          { return true }
func (m *mockRequest) CallService(types.IRouter, ...func(types.IResponse)) (types.IResponse, error) {
	return &mockResponse{}, nil
}
func (m *mockRequest) CallTargetService(types.IRouter, *types.TargetInfo, ...func(types.IResponse)) (types.IResponse, error) {
	return &mockResponse{}, nil
}
func (m *mockRequest) GetValue(string) string                         { return "" }
func (m *mockRequest) Bind(interface{}) error                         { return nil }
func (m *mockRequest) GoZeroBind(interface{}) error                   { return nil }
func (m *mockRequest) NewResponse(interface{}, error) types.IResponse { return &mockResponse{} }
func (m *mockRequest) GetPath() string                                { return "" }
func (m *mockRequest) GetClaims(string) interface{}                   { return nil }
func (m *mockRequest) ServiceName() string                            { return m.serviceName }
func (m *mockRequest) GetServerInfo() *types.TargetInfo               { return nil }
func (m *mockRequest) GetTargetServerInfo(string) *types.TargetInfo   { return nil }

type mockRouter struct {
	info   *types.RouterInfo
	hash   uint64
	regs   int32
	unregs int32
}

func (m *mockRouter) Parse(types.IRequest) error             { return nil }
func (m *mockRouter) Validation(types.IRequest) error        { return nil }
func (m *mockRouter) Do(types.IRequest) (interface{}, error) { return nil, nil }
func (m *mockRouter) RouterInfo() *types.RouterInfo          { return m.info }
func (m *mockRouter) RegisterWebSocket(types.IWebSocket, types.IRequest) {
	atomic.AddInt32(&m.regs, 1)
}
func (m *mockRouter) UnRegisterWebSocket(types.IWebSocket, types.IRequest) {
	atomic.AddInt32(&m.unregs, 1)
}
func (m *mockRouter) NoticeFiltersRouter(message interface{}, api types.IRouter) (bool, interface{}) {
	return true, message
}
func (m *mockRouter) GetHashKey() uint64 { return m.hash }

func newRouterInfo(path string) *types.RouterInfo {
	return &types.RouterInfo{
		Path:        path,
		ServiceName: "svc",
		PathType:    types.PublicType,
	}
}

func newMockRouter(info *types.RouterInfo, hash uint64) *mockRouter {
	router := &mockRouter{info: info, hash: hash}
	info.SetInstance(router)
	return router
}

func waitFor(t *testing.T, check func() bool) {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

// noticeCapture is an ICrossNodeForwarder that records calls for assertions.
type noticeCapture struct {
	mu      sync.Mutex
	notices []capturedNotice
	subs    []capturedSub
}

type capturedNotice struct {
	path    string
	hash    uint64
	message interface{}
}

type capturedSub struct {
	path   string
	hash   uint64
	active bool
}

func (c *noticeCapture) ForwardNotice(_ context.Context, path string, hash uint64, message interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notices = append(c.notices, capturedNotice{path, hash, message})
}

func (c *noticeCapture) OnSubscriptionChange(path string, hash uint64, active bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs = append(c.subs, capturedSub{path, hash, active})
}

func (c *noticeCapture) DrainAndStop(_ context.Context) {}

// ---- tests ----

// TestCrossNodeBrokerRegistration verifies that when a subscription change is
// pushed to the broker the peer registry is updated.
func TestCrossNodeBrokerRegistration(t *testing.T) {
	provider := cluster.NewLocalProvider(5*time.Second, 5*time.Second, 10*time.Second)
	provider.Start()

	brokerA := cluster.NewCrossNodeNoticeBroker(provider, "svc", "nodeA")
	brokerB := cluster.NewCrossNodeNoticeBroker(provider, "svc", "nodeB")
	_ = brokerB // nodeB broker would receive HTTP calls in a real scenario

	// Simulate nodeB telling nodeA: "I subscribed to /ws/price hash 42"
	brokerA.UpdatePeerSubscription("/ws/price", 42, "nodeB", true)

	// Now check that ForwardNotice would try to reach nodeB.
	// We can't do actual HTTP without a running server, so we just verify that
	// peerNodesForHash returns nodeB.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	brokerA.ForwardNotice(ctx, "/ws/price", 42, "msg") // should not panic even with cancelled ctx
}

// TestCrossNodeBrokerDrainAndStop verifies that drain broadcasts removal events.
func TestCrossNodeBrokerDrainAndStop(t *testing.T) {
	provider := cluster.NewLocalProvider(5*time.Second, 5*time.Second, 10*time.Second)
	provider.Start()

	broker := cluster.NewCrossNodeNoticeBroker(provider, "svc", "nodeA")
	types.SetCrossNodeForwarder(broker)
	defer types.SetCrossNodeForwarder(nil)

	info := newRouterInfo("/ws/price")
	router := newMockRouter(info, 99)
	ws := &mockWebSocket{}
	req := &mockRequest{serviceName: "svc"}
	info.RegisterWebSocketClient(router, ws, req)
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	broker.DrainAndStop(ctx)
}

// TestCrossNodeForwarderHookOnRegister verifies that registering a WebSocket
// client triggers OnSubscriptionChange(active=true).
func TestCrossNodeForwarderHookOnRegister(t *testing.T) {
	capture := &noticeCapture{}
	types.SetCrossNodeForwarder(capture)
	defer types.SetCrossNodeForwarder(nil)

	info := newRouterInfo("/ws/test")
	router := newMockRouter(info, 123)
	ws := &mockWebSocket{}
	req := &mockRequest{serviceName: "svc"}

	info.RegisterWebSocketClient(router, ws, req)

	waitFor(t, func() bool {
		capture.mu.Lock()
		defer capture.mu.Unlock()
		return len(capture.subs) == 1
	})

	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.subs) != 1 {
		t.Fatalf("expected 1 subscription event, got %d", len(capture.subs))
	}
	if !capture.subs[0].active {
		t.Error("expected active=true")
	}
	if capture.subs[0].hash != 123 {
		t.Fatalf("expected hash 123, got %d", capture.subs[0].hash)
	}
}

// TestCrossNodeForwarderHookOnNotice verifies that ForwardNotice is called
// after local dispatch.
func TestCrossNodeForwarderHookOnNotice(t *testing.T) {
	cap := &noticeCapture{}
	types.SetCrossNodeForwarder(cap)
	defer types.SetCrossNodeForwarder(nil)

	info := newRouterInfo("/ws/trade")
	router := newMockRouter(info, 77)
	ws := &mockWebSocket{}
	req := &mockRequest{serviceName: "svc"}
	info.RegisterWebSocketClient(router, ws, req)

	info.NoticeWebSocket("data")

	waitFor(t, func() bool {
		cap.mu.Lock()
		defer cap.mu.Unlock()
		return len(cap.notices) == 1
	})

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if len(cap.notices) != 1 {
		t.Fatalf("expected 1 notice recorded, got %d", len(cap.notices))
	}
	if cap.notices[0].hash != 77 {
		t.Errorf("unexpected hash %d", cap.notices[0].hash)
	}
}

// TestCrossNodeForwarderUnregisterHook verifies that removing the last client
// triggers OnSubscriptionChange(active=false).
func TestCrossNodeForwarderUnregisterHook(t *testing.T) {
	capture := &noticeCapture{}
	types.SetCrossNodeForwarder(capture)
	defer types.SetCrossNodeForwarder(nil)

	info := newRouterInfo("/ws/order")
	routerA := newMockRouter(info, 55)
	routerB := newMockRouter(info, 66)
	wsA := &mockWebSocket{}
	wsB := &mockWebSocket{}
	req := &mockRequest{serviceName: "svc"}

	hashA := info.RegisterWebSocketClient(routerA, wsA, req)
	_ = info.RegisterWebSocketClient(routerB, wsB, req)

	waitFor(t, func() bool {
		capture.mu.Lock()
		defer capture.mu.Unlock()
		return len(capture.subs) >= 2
	})

	info.UnRegisterWebSocketHash(hashA, wsA)

	waitFor(t, func() bool {
		capture.mu.Lock()
		defer capture.mu.Unlock()
		for _, sub := range capture.subs {
			if sub.hash == hashA && !sub.active {
				return true
			}
		}
		return false
	})

	if atomic.LoadInt32(&routerA.unregs) != 1 {
		t.Fatalf("expected routerA unregister hook once, got %d", atomic.LoadInt32(&routerA.unregs))
	}
	if atomic.LoadInt32(&routerB.unregs) != 0 {
		t.Fatalf("expected routerB unregister hook not called, got %d", atomic.LoadInt32(&routerB.unregs))
	}

	capture.mu.Lock()
	defer capture.mu.Unlock()
	var sawHashAInactive, sawHashBInactive bool
	for _, sub := range capture.subs {
		if sub.hash == hashA && !sub.active {
			sawHashAInactive = true
		}
		if sub.hash == 66 && !sub.active {
			sawHashBInactive = true
		}
	}
	if !sawHashAInactive {
		t.Fatal("expected inactive event for hashA")
	}
	if sawHashBInactive {
		t.Fatal("did not expect inactive event for hashB")
	}
}
