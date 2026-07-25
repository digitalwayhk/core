package routecache

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
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

type blockingGenerationRedis struct {
	*fakeRedisClient
	mu      sync.Mutex
	armed   bool
	blocked chan struct{}
	release chan struct{}
}

func newBlockingGenerationRedis() *blockingGenerationRedis {
	return &blockingGenerationRedis{fakeRedisClient: newFakeRedisClient()}
}

func (r *blockingGenerationRedis) blockNextGenerationRead() (<-chan struct{}, chan<- struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.armed = true
	r.blocked = make(chan struct{})
	r.release = make(chan struct{})
	return r.blocked, r.release
}

func (r *blockingGenerationRedis) GetCtx(ctx context.Context, key string) (string, error) {
	value, err := r.fakeRedisClient.GetCtx(ctx, key)
	r.mu.Lock()
	if !r.armed || !strings.Contains(key, "__meta:generation:") {
		r.mu.Unlock()
		return value, err
	}
	r.armed = false
	blocked := r.blocked
	release := r.release
	r.mu.Unlock()
	close(blocked)
	<-release
	return value, err
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

func TestConcurrentEnableRouteDoesNotRollBackGeneration(t *testing.T) {
	redisClient := newBlockingGenerationRedis()
	bridge := newFakeInvalidationBridge(newFakeInvalidationBus())
	manager, err := NewManager("service-a", sharedCacheConfig(),
		WithRedisClient(redisClient), WithInvalidationBridge(bridge))
	require.NoError(t, err)
	t.Cleanup(manager.Close)
	require.NoError(t, manager.EnableRoute("/api/items", time.Minute))
	require.NoError(t, manager.Set("/api/items", "same", map[string]int{"value": 1}, time.Minute))

	blocked, release := redisClient.blockNextGenerationRead()
	enableDone := make(chan error, 1)
	go func() {
		enableDone <- manager.EnableRoute("/api/items", time.Minute)
	}()
	<-blocked
	require.NoError(t, manager.DeleteRoute("/api/items"))
	close(release)
	require.NoError(t, <-enableDone)

	redisGeneration, err := manager.redis.Generation(context.Background(), "service-a", "/api/items")
	require.NoError(t, err)
	assert.Equal(t, redisGeneration, manager.routeGeneration("/api/items"))
	value, ok, err := manager.Get("/api/items", "same")
	require.NoError(t, err)
	assert.False(t, ok, "并发 EnableRoute 不得回退本地 generation 并命中失效值: %v", value)
}

func TestRecoverDoesNotRollBackConcurrentInvalidationGeneration(t *testing.T) {
	redisClient := newBlockingGenerationRedis()
	bridge := newFakeInvalidationBridge(newFakeInvalidationBus())
	manager, err := NewManager("service-a", sharedCacheConfig(),
		WithRedisClient(redisClient), WithInvalidationBridge(bridge))
	require.NoError(t, err)
	t.Cleanup(manager.Close)
	require.NoError(t, manager.EnableRoute("/api/items", time.Minute))
	require.NoError(t, manager.Set("/api/items", "same", map[string]int{"value": 1}, time.Minute))
	manager.MarkInvalidationUnavailable()

	blocked, release := redisClient.blockNextGenerationRead()
	type recoveryResult struct {
		recovered bool
		err       error
	}
	recoverDone := make(chan recoveryResult, 1)
	go func() {
		recovered, recoverErr := manager.Recover(context.Background())
		recoverDone <- recoveryResult{recovered: recovered, err: recoverErr}
	}()
	<-blocked
	generation, err := manager.redis.IncrementGeneration(context.Background(), "service-a", "/api/items")
	require.NoError(t, err)
	data, err := json.Marshal(invalidationEvent{
		Service: "service-a", Route: "/api/items", Generation: generation,
	})
	require.NoError(t, err)
	manager.handleInvalidation(event.NewEnvelope("service-a", invalidationEventType("service-a"), data))
	close(release)

	result := <-recoverDone
	require.NoError(t, result.err)
	require.True(t, result.recovered)
	redisGeneration, err := manager.redis.Generation(context.Background(), "service-a", "/api/items")
	require.NoError(t, err)
	assert.Equal(t, redisGeneration, manager.routeGeneration("/api/items"))
	assert.Equal(t, generation, redisGeneration)
	value, ok, err := manager.Get("/api/items", "same")
	require.NoError(t, err)
	assert.False(t, ok, "Recover 不得命中失效世代的值: %v", value)
}

func TestRecoverDoesNotRecreateRouteRemovedAfterSnapshot(t *testing.T) {
	redisClient := newBlockingGenerationRedis()
	bridge := newFakeInvalidationBridge(newFakeInvalidationBus())
	manager, err := NewManager("service-a", sharedCacheConfig(),
		WithRedisClient(redisClient), WithInvalidationBridge(bridge))
	require.NoError(t, err)
	t.Cleanup(manager.Close)
	require.NoError(t, manager.EnableRoute("/api/items", time.Minute))
	manager.MarkInvalidationUnavailable()

	blocked, release := redisClient.blockNextGenerationRead()
	type recoveryResult struct {
		recovered bool
		err       error
	}
	recoverDone := make(chan recoveryResult, 1)
	go func() {
		recovered, recoverErr := manager.Recover(context.Background())
		recoverDone <- recoveryResult{recovered: recovered, err: recoverErr}
	}()
	<-blocked
	manager.routesMu.Lock()
	delete(manager.routes, "/api/items")
	manager.routesMu.Unlock()
	close(release)

	result := <-recoverDone
	require.NoError(t, result.err)
	require.True(t, result.recovered)
	manager.routesMu.RLock()
	_, exists := manager.routes["/api/items"]
	manager.routesMu.RUnlock()
	assert.False(t, exists, "Recover 不得用迟到的 generation 重建已删除路由")
}

func TestLocalConcurrentDeleteRouteDoesNotLoseGeneration(t *testing.T) {
	cfg := config.RouteCacheConfig{
		Mode: "local",
		TTL:  time.Minute,
		L1:   config.RouteCacheL1Config{Limit: 32},
	}
	cfg.ApplyDefaults()
	manager, err := NewManager("service-a", cfg)
	require.NoError(t, err)
	t.Cleanup(manager.Close)
	require.NoError(t, manager.EnableRoute("/api/items", time.Minute))

	const deletes = 256
	start := make(chan struct{})
	errors := make(chan error, deletes)
	var group sync.WaitGroup
	for range deletes {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			errors <- manager.DeleteRoute("/api/items")
		}()
	}
	close(start)
	group.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	assert.Equal(t, uint64(deletes+1), manager.routeGeneration("/api/items"))
}
