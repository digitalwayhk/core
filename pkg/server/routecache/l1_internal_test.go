package routecache

import (
	"reflect"
	"runtime/debug"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestL1CacheDoesNotKeepUnboundedKeyIndex(t *testing.T) {
	_, exists := reflect.TypeOf(l1Cache{}).FieldByName("keys")
	assert.False(t, exists, "L1 不应保留无法跟随 TTL/LRU 淘汰的 key 索引")
}

func TestL1AutoBudgetUsesEffectiveMemoryLimit(t *testing.T) {
	oldLimit := debug.SetMemoryLimit(512 << 20)
	t.Cleanup(func() { debug.SetMemoryLimit(oldLimit) })
	cfg := config.RouteCacheConfig{Mode: "local", TTL: time.Second}
	cfg.ApplyDefaults()
	manager, err := NewManager("auto-budget-service", cfg)
	require.NoError(t, err)
	t.Cleanup(manager.Close)

	assert.Equal(t, int64(16<<20), manager.config.L1.MaxBytes)
	require.NoError(t, manager.EnableRoute("/api/items", time.Second))
	require.NoError(t, manager.Set("/api/items", "key", "value", time.Second))
	_, ok, err := manager.Get("/api/items", "key")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestL1MaxBytesIsSharedAcrossServiceContexts(t *testing.T) {
	newManager := func(service string) *Manager {
		cfg := config.RouteCacheConfig{
			Mode: "local",
			TTL:  time.Second,
			L1: config.RouteCacheL1Config{
				MaxEntries:    16,
				MaxValueBytes: 64,
				MaxBytes:      11,
			},
		}
		cfg.ApplyDefaults()
		manager, err := NewManager(service, cfg)
		require.NoError(t, err)
		t.Cleanup(manager.Close)
		require.NoError(t, manager.EnableRoute("/api/items", time.Second))
		return manager
	}
	first := newManager("budget-service-a")
	second := newManager("budget-service-b")
	require.NoError(t, first.Set("/api/items", "first", "1234", time.Second))
	require.NoError(t, second.Set("/api/items", "second", "5678", time.Second))

	_, firstOK, err := first.Get("/api/items", "first")
	require.NoError(t, err)
	assert.True(t, firstOK)
	_, secondOK, err := second.Get("/api/items", "second")
	require.NoError(t, err)
	assert.False(t, secondOK)
}
