package routecache

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeInvalidationBus struct {
	mu      sync.Mutex
	bridges map[*fakeInvalidationBridge]struct{}
	ready   bool
}

func newFakeInvalidationBus() *fakeInvalidationBus {
	return &fakeInvalidationBus{bridges: make(map[*fakeInvalidationBridge]struct{}), ready: true}
}

type fakeInvalidationBridge struct {
	bus      *fakeInvalidationBus
	mu       sync.RWMutex
	handlers map[string][]event.Handler
	ready    bool
}

func newFakeInvalidationBridge(bus *fakeInvalidationBus) *fakeInvalidationBridge {
	bridge := &fakeInvalidationBridge{bus: bus, handlers: make(map[string][]event.Handler), ready: true}
	bus.mu.Lock()
	bus.bridges[bridge] = struct{}{}
	bus.mu.Unlock()
	return bridge
}

func (b *fakeInvalidationBridge) Subscribe(eventType string, handler event.Handler) (func(), error) {
	b.mu.Lock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
	b.mu.Unlock()
	return func() {}, nil
}

func (b *fakeInvalidationBridge) SubscribeExternal(context.Context, string) (func(), error) {
	b.mu.RLock()
	ready := b.ready
	b.mu.RUnlock()
	if !ready {
		return nil, errors.New("external subscription unavailable")
	}
	return func() {}, nil
}

func (b *fakeInvalidationBridge) Publish(_ context.Context, request event.PublishRequest) error {
	b.mu.RLock()
	ready := b.ready
	b.mu.RUnlock()
	if request.External && !ready {
		return errors.New("external publish unavailable")
	}
	env := *request.Envelope
	if request.BuildData != nil {
		data, err := request.BuildData()
		if err != nil {
			return err
		}
		env.Data = data
	}
	b.bus.mu.Lock()
	bridges := make([]*fakeInvalidationBridge, 0, len(b.bus.bridges))
	for bridge := range b.bus.bridges {
		bridges = append(bridges, bridge)
	}
	b.bus.mu.Unlock()
	for _, bridge := range bridges {
		bridge.mu.RLock()
		handlers := append([]event.Handler(nil), bridge.handlers[env.Type]...)
		bridge.mu.RUnlock()
		for _, handler := range handlers {
			handler(&env)
		}
	}
	return nil
}

func (b *fakeInvalidationBridge) setExternalReady(ready bool) {
	b.mu.Lock()
	b.ready = ready
	b.mu.Unlock()
}

func TestInvalidationClearsPeerL1L2(t *testing.T) {
	redisClient := newFakeRedisClient()
	bus := newFakeInvalidationBus()
	first, err := NewManager("service-a", sharedCacheConfig(),
		WithRedisClient(redisClient), WithInvalidationBridge(newFakeInvalidationBridge(bus)))
	require.NoError(t, err)
	t.Cleanup(first.Close)
	second, err := NewManager("service-a", sharedCacheConfig(),
		WithRedisClient(redisClient), WithInvalidationBridge(newFakeInvalidationBridge(bus)))
	require.NoError(t, err)
	t.Cleanup(second.Close)
	for _, manager := range []*Manager{first, second} {
		require.NoError(t, manager.EnableRoute("/api/items", time.Minute))
	}
	require.NoError(t, first.Set("/api/items", "same", map[string]int{"value": 1}, time.Minute))
	_, ok, err := second.Get("/api/items", "same")
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, first.Set("/api/items", "same", map[string]int{"value": 2}, time.Minute))

	value, ok, err := second.Get("/api/items", "same")
	require.NoError(t, err)
	require.True(t, ok)
	assert.JSONEq(t, `{"value":2}`, string(value.(json.RawMessage)))
}

func TestInvalidationIsIdempotent(t *testing.T) {
	redisClient := newFakeRedisClient()
	bridge := newFakeInvalidationBridge(newFakeInvalidationBus())
	manager, err := NewManager("service-a", sharedCacheConfig(),
		WithRedisClient(redisClient), WithInvalidationBridge(bridge))
	require.NoError(t, err)
	t.Cleanup(manager.Close)
	require.NoError(t, manager.EnableRoute("/api/items", time.Minute))
	require.NoError(t, manager.Set("/api/items", "same", "value", time.Minute))
	key, _, err := manager.cacheKey("/api/items", "same")
	require.NoError(t, err)
	payload := invalidationEvent{Service: "service-a", Route: "/api/items", Key: key, Generation: 1}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	env := event.NewEnvelope("service-a", invalidationEventType("service-a"), data)

	manager.handleInvalidation(env)
	manager.handleInvalidation(env)

	_, ok := manager.l1.Get(key)
	assert.False(t, ok)
}

func TestSharedGenerationRestartDoesNotServePreInvalidationKeys(t *testing.T) {
	redisClient := newFakeRedisClient()
	bus := newFakeInvalidationBus()
	first, err := NewManager("service-a", sharedCacheConfig(),
		WithRedisClient(redisClient), WithInvalidationBridge(newFakeInvalidationBridge(bus)))
	require.NoError(t, err)
	t.Cleanup(first.Close)
	require.NoError(t, first.EnableRoute("/api/items", time.Minute))
	require.NoError(t, first.Set("/api/items", "same", map[string]int{"value": 1}, time.Minute))
	require.NoError(t, first.DeleteRoute("/api/items"))

	second, err := NewManager("service-a", sharedCacheConfig(),
		WithRedisClient(redisClient), WithInvalidationBridge(newFakeInvalidationBridge(bus)))
	require.NoError(t, err)
	t.Cleanup(second.Close)
	require.NoError(t, second.EnableRoute("/api/items", time.Minute))

	value, ok, err := second.Get("/api/items", "same")
	require.NoError(t, err)
	assert.False(t, ok, "重启节点不得重新使用 Redis 中失效世代的值: %v", value)
}
