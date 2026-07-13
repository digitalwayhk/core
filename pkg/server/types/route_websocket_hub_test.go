package types

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type hubTestWebSocket struct {
	mu     sync.Mutex
	closed bool
	events []string
}

func (w *hubTestWebSocket) Send(eventName, _ string, _ interface{}) {
	w.mu.Lock()
	w.events = append(w.events, eventName)
	w.mu.Unlock()
}

func (w *hubTestWebSocket) IsClosed() bool {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	return closed
}

func (w *hubTestWebSocket) close() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
}

func (w *hubTestWebSocket) eventCount(eventName string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	count := 0
	for _, current := range w.events {
		if current == eventName {
			count++
		}
	}
	return count
}

type hubTestRouter struct {
	info        *RouterInfo
	hash        uint64
	registers   atomic.Int32
	unregisters atomic.Int32
}

func (*hubTestRouter) Parse(IRequest) error             { return nil }
func (*hubTestRouter) Validation(IRequest) error        { return nil }
func (*hubTestRouter) Do(IRequest) (interface{}, error) { return nil, nil }
func (r *hubTestRouter) RouterInfo() *RouterInfo        { return r.info }
func (r *hubTestRouter) GetHashKey() uint64             { return r.hash }
func (r *hubTestRouter) RegisterWebSocket(IWebSocket, IRequest) {
	r.registers.Add(1)
}
func (r *hubTestRouter) UnRegisterWebSocket(IWebSocket, IRequest) {
	r.unregisters.Add(1)
}
func (*hubTestRouter) NoticeFiltersRouter(message interface{}, _ IRouter) (bool, interface{}) {
	return true, message
}

func newHubTestRuntime(t *testing.T, service string) (*event.ServiceEventBridge, *RouteWebSocketHub) {
	t.Helper()
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})
	hub := NewRouteWebSocketHub(service, bridge)
	t.Cleanup(func() {
		require.NoError(t, hub.Close(context.Background()))
		require.NoError(t, bridge.Close(context.Background()))
	})
	return bridge, hub
}

func newHubTestRoute(service, path string) *RouterInfo {
	return &RouterInfo{Path: path, ServiceName: service, PathType: PublicType}
}

func waitForHub(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待 RouteWebSocketHub 条件超时")
}

func TestRouteWebSocketHubSeparatesHashesInSameShard(t *testing.T) {
	_, hub := newHubTestRuntime(t, "service-a")
	info := newHubTestRoute("service-a", "/ws/orders")
	firstRouter := &hubTestRouter{info: info, hash: 1}
	secondRouter := &hubTestRouter{info: info, hash: 129}
	info.SetInstance(firstRouter)
	firstClient := &hubTestWebSocket{}
	secondClient := &hubTestWebSocket{}
	require.Equal(t, uint64(1), hub.Register(info, firstRouter, firstClient, &shardTestRequest{}))
	require.Equal(t, uint64(129), hub.Register(info, secondRouter, secondClient, &shardTestRequest{}))

	hub.ExecuteLocalNotice(info, 1, "first")
	waitForHub(t, func() bool { return firstClient.eventCount("1") == 1 })
	assert.Equal(t, 0, secondClient.eventCount("1"))

	hub.ExecuteLocalNotice(info, 129, "second")
	waitForHub(t, func() bool { return secondClient.eventCount("129") == 1 })
	assert.Equal(t, 0, firstClient.eventCount("129"))
}

func TestRouteWebSocketHubAllowsClientOnMultipleHashes(t *testing.T) {
	_, hub := newHubTestRuntime(t, "service-a")
	info := newHubTestRoute("service-a", "/ws/orders")
	firstRouter := &hubTestRouter{info: info, hash: 2}
	secondRouter := &hubTestRouter{info: info, hash: 130}
	info.SetInstance(firstRouter)
	client := &hubTestWebSocket{}
	hub.Register(info, firstRouter, client, &shardTestRequest{})
	hub.Register(info, secondRouter, client, &shardTestRequest{})

	hub.ExecuteLocalNotice(info, 2, "first")
	hub.ExecuteLocalNotice(info, 130, "second")
	waitForHub(t, func() bool {
		return client.eventCount("2") == 1 && client.eventCount("130") == 1
	})
}

func TestRouteWebSocketHubDuplicateRegisterIsIdempotent(t *testing.T) {
	_, hub := newHubTestRuntime(t, "service-a")
	info := newHubTestRoute("service-a", "/ws/orders")
	router := &hubTestRouter{info: info, hash: 3}
	info.SetInstance(router)
	client := &hubTestWebSocket{}
	req := &shardTestRequest{}

	hub.Register(info, router, client, req)
	hub.Register(info, router, client, req)

	assert.Equal(t, int32(1), router.registers.Load())
	assert.Equal(t, 1, hub.ActiveClientCount(info))
}

func TestRouteWebSocketHubCleanupUpdatesSubscriptionState(t *testing.T) {
	bridge, hub := newHubTestRuntime(t, "service-a")
	info := newHubTestRoute("service-a", "/ws/orders")
	router := &hubTestRouter{info: info, hash: 4}
	info.SetInstance(router)
	client := &hubTestWebSocket{}
	var statesMu sync.Mutex
	states := make([]bool, 0, 2)
	cancel, err := bridge.Subscribe(routeWebSocketSubscriptionEventType("service-a"), func(env *event.Envelope) {
		payload := routeWebSocketSubscriptionEvent{}
		if json.Unmarshal(env.Data, &payload) == nil && payload.Hash == 4 {
			statesMu.Lock()
			states = append(states, payload.Active)
			statesMu.Unlock()
		}
	})
	require.NoError(t, err)
	defer cancel()

	hub.Register(info, router, client, &shardTestRequest{})
	client.close()
	hub.CleanupDeadConnections(info)

	statesMu.Lock()
	assert.Equal(t, []bool{true, false}, states)
	statesMu.Unlock()
	assert.Empty(t, hub.SubscribedHashes(info))
	assert.Equal(t, int32(1), router.unregisters.Load())
}

func TestRouteWebSocketHubControlEventsKeepActiveInactiveOrder(t *testing.T) {
	bridge, hub := newHubTestRuntime(t, "service-a")
	info := newHubTestRoute("service-a", "/ws/orders")
	router := &hubTestRouter{info: info, hash: 5}
	info.SetInstance(router)
	client := &hubTestWebSocket{}
	var mu sync.Mutex
	states := make([]bool, 0, 2)
	cancel, err := bridge.Subscribe(routeWebSocketSubscriptionEventType("service-a"), func(env *event.Envelope) {
		payload := routeWebSocketSubscriptionEvent{}
		if json.Unmarshal(env.Data, &payload) == nil && payload.Hash == 5 {
			mu.Lock()
			states = append(states, payload.Active)
			mu.Unlock()
		}
	})
	require.NoError(t, err)
	defer cancel()

	hub.Register(info, router, client, &shardTestRequest{})
	hub.Unregister(info, 5, client)

	mu.Lock()
	assert.Equal(t, []bool{true, false}, states)
	mu.Unlock()
}

func TestRouteWebSocketHubIsIsolatedPerService(t *testing.T) {
	_, firstHub := newHubTestRuntime(t, "service-a")
	_, secondHub := newHubTestRuntime(t, "service-b")
	firstInfo := newHubTestRoute("service-a", "/ws/orders")
	secondInfo := newHubTestRoute("service-b", "/ws/orders")
	firstRouter := &hubTestRouter{info: firstInfo, hash: 7}
	secondRouter := &hubTestRouter{info: secondInfo, hash: 7}
	firstInfo.SetInstance(firstRouter)
	secondInfo.SetInstance(secondRouter)
	firstClient := &hubTestWebSocket{}
	secondClient := &hubTestWebSocket{}
	firstHub.Register(firstInfo, firstRouter, firstClient, &shardTestRequest{})
	secondHub.Register(secondInfo, secondRouter, secondClient, &shardTestRequest{})

	firstHub.ExecuteLocalNotice(firstInfo, 7, "first")
	waitForHub(t, func() bool { return firstClient.eventCount("7") == 1 })
	assert.Equal(t, 0, secondClient.eventCount("7"))
}

func TestRouteWebSocketHubCloseWithoutInitializationIsSafe(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})
	hub := NewRouteWebSocketHub("service-a", bridge)

	require.NoError(t, hub.Close(context.Background()))
	require.NoError(t, hub.Close(context.Background()))
	require.NoError(t, bridge.Close(context.Background()))
}
