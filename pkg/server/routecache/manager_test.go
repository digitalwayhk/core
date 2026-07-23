package routecache_test

import (
	"encoding/json"
	"errors"
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

func newWeightedL1Manager(t *testing.T, maxEntries int, maxValueBytes, maxBytes int64) *routecache.Manager {
	t.Helper()
	cfg := config.RouteCacheConfig{
		Mode: "local",
		TTL:  time.Second,
		L1: config.RouteCacheL1Config{
			MaxEntries:    maxEntries,
			MaxValueBytes: maxValueBytes,
			MaxBytes:      maxBytes,
		},
	}
	cfg.ApplyDefaults()
	manager, err := routecache.NewManager("weighted-service", cfg)
	require.NoError(t, err)
	t.Cleanup(manager.Close)
	require.NoError(t, manager.EnableRoute("/api/items", cfg.TTL))
	return manager
}

func TestRouteCachePureL1ReturnsSerializedValue(t *testing.T) {
	manager := newWeightedL1Manager(t, 16, 1024, 4096)
	require.NoError(t, manager.Set("/api/items", "same-type", map[string]string{"name": "item"}, time.Second))

	value, ok, err := manager.Get("/api/items", "same-type")
	require.NoError(t, err)
	require.True(t, ok)
	raw, ok := value.(json.RawMessage)
	require.True(t, ok)
	assert.JSONEq(t, `{"name":"item"}`, string(raw))
}

func TestRouteCacheSkipsValueLargerThanMaxValueBytes(t *testing.T) {
	manager := newWeightedL1Manager(t, 16, 8, 4096)
	require.NoError(t, manager.Set("/api/items", "oversized", "0123456789", time.Second))

	_, ok, err := manager.Get("/api/items", "oversized")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRouteCacheL1EvictsToMaxBytes(t *testing.T) {
	manager := newWeightedL1Manager(t, 16, 64, 11)
	require.NoError(t, manager.Set("/api/items", "first", "1234", time.Second))
	require.NoError(t, manager.Set("/api/items", "second", "5678", time.Second))

	_, firstExists, err := manager.Get("/api/items", "first")
	require.NoError(t, err)
	assert.False(t, firstExists)
	_, secondExists, err := manager.Get("/api/items", "second")
	require.NoError(t, err)
	assert.True(t, secondExists)
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
	raw, ok := value.(json.RawMessage)
	require.True(t, ok)
	assert.JSONEq(t, `"one"`, string(raw))
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
		raw, ok := value.(json.RawMessage)
		require.True(t, ok)
		assert.JSONEq(t, `"loaded"`, string(raw))
	}
}

func TestRouteCacheTakeBestEffortLoadsSameKeyOnce(t *testing.T) {
	manager := newL1Manager(t, 16)
	var loads atomic.Int32
	loaderEntered := make(chan struct{})
	releaseLoader := make(chan struct{})
	start := make(chan struct{})
	results := make(chan interface{}, 16)
	errors := make(chan error, 16)
	var wg sync.WaitGroup

	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			value, err := manager.TakeBestEffort("/api/items", preferredCacheKey{}, time.Second, func() (interface{}, error) {
				if loads.Add(1) == 1 {
					close(loaderEntered)
				}
				<-releaseLoader
				return "loaded", nil
			})
			results <- value
			errors <- err
		}()
	}

	close(start)
	<-loaderEntered
	close(releaseLoader)
	wg.Wait()
	close(results)
	close(errors)

	assert.Equal(t, int32(1), loads.Load())
	for err := range errors {
		require.NoError(t, err)
	}
	for value := range results {
		raw, ok := value.(json.RawMessage)
		require.True(t, ok)
		assert.JSONEq(t, `"loaded"`, string(raw))
	}
}

func TestRouteCacheTakeBestEffortDoesNotCacheLoaderError(t *testing.T) {
	manager := newL1Manager(t, 16)
	wantErr := errors.New("load failed")
	var loads atomic.Int32

	for range 2 {
		value, err := manager.TakeBestEffort("/api/items", "error-key", time.Second, func() (interface{}, error) {
			loads.Add(1)
			return nil, wantErr
		})
		assert.Nil(t, value)
		assert.ErrorIs(t, err, wantErr)
	}

	assert.Equal(t, int32(2), loads.Load())
}
