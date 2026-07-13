package routecache

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	zeroredis "github.com/zeromicro/go-zero/core/stores/redis"
)

type fakeRedisClient struct {
	mu        sync.Mutex
	available bool
	emptyMiss bool
	values    map[string]string
}

func newFakeRedisClient() *fakeRedisClient {
	return &fakeRedisClient{available: true, values: make(map[string]string)}
}

func (f *fakeRedisClient) GetCtx(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.available {
		return "", errors.New("redis unavailable")
	}
	value, ok := f.values[key]
	if !ok {
		if f.emptyMiss {
			return "", nil
		}
		return "", redis.Nil
	}
	return value, nil
}

func TestGenerationSupportsGoZeroEmptyMiss(t *testing.T) {
	client := newFakeRedisClient()
	client.emptyMiss = true
	l3 := NewRedisL3(client, config.RouteCacheRedisConfig{Prefix: "test:routecache"})

	generation, err := l3.Generation(context.Background(), "service-a", "/items")

	require.NoError(t, err)
	assert.Equal(t, uint64(1), generation)
}

func (f *fakeRedisClient) SetexCtx(_ context.Context, key, value string, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.available {
		return errors.New("redis unavailable")
	}
	f.values[key] = value
	return nil
}

func (f *fakeRedisClient) SetnxCtx(_ context.Context, key, value string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.available {
		return false, errors.New("redis unavailable")
	}
	if _, exists := f.values[key]; exists {
		return false, nil
	}
	f.values[key] = value
	return true, nil
}

func (f *fakeRedisClient) IncrCtx(_ context.Context, key string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.available {
		return 0, errors.New("redis unavailable")
	}
	value, err := strconv.ParseInt(f.values[key], 10, 64)
	if err != nil {
		return 0, err
	}
	value++
	f.values[key] = strconv.FormatInt(value, 10)
	return value, nil
}

func (f *fakeRedisClient) DelCtx(_ context.Context, keys ...string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.available {
		return 0, errors.New("redis unavailable")
	}
	deleted := 0
	for _, key := range keys {
		if _, ok := f.values[key]; ok {
			delete(f.values, key)
			deleted++
		}
	}
	return deleted, nil
}

func (f *fakeRedisClient) PingCtx(context.Context) bool {
	f.mu.Lock()
	available := f.available
	f.mu.Unlock()
	return available
}

func (f *fakeRedisClient) setAvailable(available bool) {
	f.mu.Lock()
	f.available = available
	f.mu.Unlock()
}

func sharedCacheConfig() config.RouteCacheConfig {
	cfg := config.RouteCacheConfig{
		Mode: "shared",
		TTL:  time.Minute,
		L1:   config.RouteCacheL1Config{Limit: 32},
		Redis: config.RouteCacheRedisConfig{
			Addr:          "fake:6379",
			Prefix:        "test:routecache",
			OnUnavailable: "fail",
		},
	}
	cfg.ApplyDefaults()
	return cfg
}

func TestSharedModeWithoutRedisFailsClosed(t *testing.T) {
	cfg := sharedCacheConfig()
	cfg.Redis.Addr = ""
	bridge := newFakeInvalidationBridge(newFakeInvalidationBus())

	_, err := NewManager("service-a", cfg, WithInvalidationBridge(bridge))

	require.Error(t, err)
}

func TestSharedModeExplicitBypassDisablesAllLayers(t *testing.T) {
	cfg := sharedCacheConfig()
	cfg.Redis.Addr = ""
	cfg.Redis.OnUnavailable = "bypass"
	bridge := newFakeInvalidationBridge(newFakeInvalidationBus())

	manager, err := NewManager("service-a", cfg, WithInvalidationBridge(bridge))
	require.NoError(t, err)
	t.Cleanup(manager.Close)

	assert.Equal(t, StateBypass, manager.State())
	assert.Nil(t, manager.l1)
	assert.Nil(t, manager.l2)
}

func TestRedisFailureClearsAndPausesL1L2(t *testing.T) {
	redisClient := newFakeRedisClient()
	bridge := newFakeInvalidationBridge(newFakeInvalidationBus())
	manager, err := NewManager("service-a", sharedCacheConfig(),
		WithRedisClient(redisClient), WithInvalidationBridge(bridge))
	require.NoError(t, err)
	t.Cleanup(manager.Close)
	require.NoError(t, manager.EnableRoute("/api/items", time.Minute))
	require.NoError(t, manager.Set("/api/items", "same", "value", time.Minute))
	key, enabled, err := manager.cacheKey("/api/items", "same")
	require.NoError(t, err)
	require.True(t, enabled)
	manager.l1.Delete(key)
	redisClient.setAvailable(false)

	_, ok, err := manager.Get("/api/items", "same")
	require.Error(t, err)
	assert.False(t, ok)
	assert.Equal(t, StateDegraded, manager.State())
	_, local := manager.l1.Get(key)
	assert.False(t, local)
	assert.NoError(t, manager.Set("/api/items", "same", "ignored", time.Minute))
}

func TestRedisRecoveryWaitsForInvalidationSubscription(t *testing.T) {
	redisClient := newFakeRedisClient()
	bridge := newFakeInvalidationBridge(newFakeInvalidationBus())
	manager, err := NewManager("service-a", sharedCacheConfig(),
		WithRedisClient(redisClient), WithInvalidationBridge(bridge))
	require.NoError(t, err)
	t.Cleanup(manager.Close)
	manager.MarkInvalidationUnavailable()
	bridge.setExternalReady(false)

	recovered, err := manager.Recover(context.Background())
	require.Error(t, err)
	assert.False(t, recovered)
	assert.Equal(t, StateDegraded, manager.State())

	bridge.setExternalReady(true)
	recovered, err = manager.Recover(context.Background())
	require.NoError(t, err)
	assert.True(t, recovered)
	assert.Equal(t, StateEnabled, manager.State())
}

func TestRedisL3Integration(t *testing.T) {
	if os.Getenv("CORE_TEST_REDIS") != "1" {
		t.Skip("设置 CORE_TEST_REDIS=1 后运行真实 Redis 集成测试")
	}
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	client, err := zeroredis.NewRedis(zeroredis.RedisConf{
		Host:        addr,
		Type:        zeroredis.NodeType,
		NonBlock:    false,
		PingTimeout: 2 * time.Second,
	})
	require.NoError(t, err)
	l3 := NewRedisL3(client, config.RouteCacheRedisConfig{Prefix: "core:test:routecache"})
	key := "integration:" + time.Now().Format("150405.000000000")
	require.NoError(t, l3.Set(context.Background(), key, []byte(`{"ok":true}`), time.Minute))
	value, ok, err := l3.Get(context.Background(), key)
	require.NoError(t, err)
	require.True(t, ok)
	assert.JSONEq(t, `{"ok":true}`, string(value))
	require.NoError(t, l3.Delete(context.Background(), key))

	route := "/integration/" + time.Now().Format("150405.000000000")
	generation, err := l3.Generation(context.Background(), "integration-service", route)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), generation)
	generation, err = l3.IncrementGeneration(context.Background(), "integration-service", route)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), generation)
	generation, err = l3.Generation(context.Background(), "integration-service", route)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), generation)
}
