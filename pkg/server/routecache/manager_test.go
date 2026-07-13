package routecache_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/routecache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type preferredCacheKey struct{}

func (preferredCacheKey) GetCacheKey() string { return "preferred" }
func (preferredCacheKey) GetHashKey() uint64  { return 42 }

func newL1Manager(t *testing.T, limit int) *routecache.Manager {
	t.Helper()
	cfg := config.RouteCacheConfig{
		Mode: "local",
		TTL:  time.Second,
		L1:   config.RouteCacheL1Config{Limit: limit},
	}
	cfg.ApplyDefaults()
	manager, err := routecache.NewManager("service-a", cfg)
	require.NoError(t, err)
	t.Cleanup(manager.Close)
	require.NoError(t, manager.EnableRoute("/api/items", cfg.TTL))
	return manager
}

func TestRouteCacheKeyUsesCacheKeyBeforeHashKey(t *testing.T) {
	key, err := routecache.BuildKey(preferredCacheKey{})

	require.NoError(t, err)
	assert.Equal(t, "key:preferred", key)
}

func TestRouteCacheFallbackEncodingHasFieldBoundaries(t *testing.T) {
	first, err := routecache.BuildKey(struct {
		A string
		B string
	}{A: "ab", B: "c"})
	require.NoError(t, err)
	second, err := routecache.BuildKey(struct {
		A string
		B string
	}{A: "a", B: "bc"})
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

func TestRouteCacheL1ExpiresAndEvicts(t *testing.T) {
	manager := newL1Manager(t, 2)
	require.NoError(t, manager.Set("/api/items", "first", "one", 30*time.Millisecond))
	value, ok, err := manager.Get("/api/items", "first")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "one", value)
	time.Sleep(50 * time.Millisecond)
	_, ok, err = manager.Get("/api/items", "first")
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, manager.Set("/api/items", "a", 1, time.Second))
	require.NoError(t, manager.Set("/api/items", "b", 2, time.Second))
	require.NoError(t, manager.Set("/api/items", "c", 3, time.Second))
	_, firstExists, err := manager.Get("/api/items", "a")
	require.NoError(t, err)
	assert.False(t, firstExists)
}

func TestRouteCacheSingleFlightLoadsOnce(t *testing.T) {
	manager := newL1Manager(t, 16)
	var loads atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan interface{}, 16)
	errors := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			value, err := manager.Take("/api/items", "same", time.Second, func() (interface{}, error) {
				loads.Add(1)
				time.Sleep(20 * time.Millisecond)
				return "loaded", nil
			})
			errors <- err
			results <- value
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errors)

	assert.Equal(t, int32(1), loads.Load())
	for err := range errors {
		require.NoError(t, err)
	}
	for value := range results {
		assert.Equal(t, "loaded", value)
	}
}
