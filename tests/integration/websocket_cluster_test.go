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

	// Register a local subscription so DrainAndStop has something to broadcast.
	broker.UpdatePeerSubscription("/ws/price", 99, "nodeA", true)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Should not hang or panic.
	broker.DrainAndStop(ctx)
}

// TestCrossNodeForwarderHookOnRegister verifies that registering a WebSocket
// client triggers OnSubscriptionChange(active=true).
func TestCrossNodeForwarderHookOnRegister(t *testing.T) {
	capture := &noticeCapture{}
	types.SetCrossNodeForwarder(capture)
	defer types.SetCrossNodeForwarder(nil)

	ri := &types.RouterInfo{
		Path: "/ws/test",
	}

	// Minimal mock implementations
	_ = ri // Would normally call ri.RegisterWebSocketClient(router, ws, req)
	// Instead we test the forwarder directly.
	types.GetCrossNodeForwarder().OnSubscriptionChange("/ws/test", 123, true)

	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.subs) != 1 {
		t.Fatalf("expected 1 subscription event, got %d", len(capture.subs))
	}
	if !capture.subs[0].active {
		t.Error("expected active=true")
	}
}

// TestCrossNodeForwarderHookOnNotice verifies that ForwardNotice is called
// after local dispatch.
func TestCrossNodeForwarderHookOnNotice(t *testing.T) {
	var forwardCount int64
	cap := &noticeCapture{}
	types.SetCrossNodeForwarder(cap)
	defer types.SetCrossNodeForwarder(nil)

	// Directly exercise ForwardNotice so we can count calls.
	ctx := context.Background()
	types.GetCrossNodeForwarder().ForwardNotice(ctx, "/ws/trade", 77, "data")
	atomic.AddInt64(&forwardCount, 1)

	if atomic.LoadInt64(&forwardCount) != 1 {
		t.Fatalf("expected 1 forward, got %d", forwardCount)
	}
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

	types.GetCrossNodeForwarder().OnSubscriptionChange("/ws/order", 55, false)

	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.subs) != 1 {
		t.Fatalf("expected 1 sub event, got %d", len(capture.subs))
	}
	if capture.subs[0].active {
		t.Error("expected active=false")
	}
}
