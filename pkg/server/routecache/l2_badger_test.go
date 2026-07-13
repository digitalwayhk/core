package routecache

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testBadgerL2Config(path string) config.RouteCacheL2Config {
	return config.RouteCacheL2Config{
		Enable:           true,
		Path:             path,
		MaxBytes:         32 << 20,
		CorruptionPolicy: "fail",
	}
}

func TestBadgerL2SetGetDeleteWithTTL(t *testing.T) {
	l2, err := NewBadgerL2(testBadgerL2Config(t.TempDir()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, l2.Close()) })

	require.NoError(t, l2.Set("key", json.RawMessage(`{"value":1}`), 30*time.Millisecond))
	value, ok, err := l2.Get("key")
	require.NoError(t, err)
	require.True(t, ok)
	assert.JSONEq(t, `{"value":1}`, string(value))
	time.Sleep(50 * time.Millisecond)
	_, ok, err = l2.Get("key")
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, l2.Set("delete", json.RawMessage(`1`), time.Second))
	require.NoError(t, l2.Delete("delete"))
	_, ok, err = l2.Get("delete")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestBadgerL2RestartReadsUnexpiredValue(t *testing.T) {
	path := t.TempDir()
	first, err := NewBadgerL2(testBadgerL2Config(path))
	require.NoError(t, err)
	require.NoError(t, first.Set("restart", json.RawMessage(`{"ok":true}`), time.Minute))
	require.NoError(t, first.Close())

	second, err := NewBadgerL2(testBadgerL2Config(path))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, second.Close()) })
	value, ok, err := second.Get("restart")
	require.NoError(t, err)
	require.True(t, ok)
	assert.JSONEq(t, `{"ok":true}`, string(value))
}

func TestBadgerL2HasNoWriteBehindQueue(t *testing.T) {
	l2, err := NewBadgerL2(testBadgerL2Config(t.TempDir()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, l2.Close()) })
	require.NoError(t, l2.Set("cache-only", json.RawMessage(`1`), time.Minute))

	var keys [][]byte
	require.NoError(t, l2.db.View(func(txn *badger.Txn) error {
		iterator := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()
		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			keys = append(keys, append([]byte(nil), iterator.Item().Key()...))
		}
		return nil
	}))
	require.Len(t, keys, 1)
	assert.True(t, bytes.HasPrefix(keys[0], []byte(badgerL2KeyPrefix)))
	assert.NotContains(t, strings.ToLower(string(keys[0])), "sync")
	assert.NotContains(t, strings.ToLower(string(keys[0])), "queue")
}

func TestBadgerL2CorruptionResetRequiresExplicitPolicy(t *testing.T) {
	path := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(path, "MANIFEST"), []byte("broken"), 0o600))

	_, err := NewBadgerL2(testBadgerL2Config(path))
	require.Error(t, err)
	_, statErr := os.Stat(filepath.Join(path, "MANIFEST"))
	require.NoError(t, statErr, "fail 策略必须保留损坏现场")

	cfg := testBadgerL2Config(path)
	cfg.CorruptionPolicy = "reset"
	l2, err := NewBadgerL2(cfg)
	require.NoError(t, err)
	require.NoError(t, l2.Close())
}

func TestRouteCachePromotesL2HitToL1(t *testing.T) {
	cfg := config.RouteCacheConfig{
		Mode: "local",
		TTL:  time.Minute,
		L1:   config.RouteCacheL1Config{Limit: 16},
		L2:   testBadgerL2Config(t.TempDir()),
	}
	cfg.ApplyDefaults()
	manager, err := NewManager("service-a", cfg)
	require.NoError(t, err)
	t.Cleanup(manager.Close)
	require.NoError(t, manager.EnableRoute("/api/items", time.Minute))
	require.NoError(t, manager.Set("/api/items", "same", map[string]int{"value": 1}, time.Minute))
	immediate, ok, err := manager.Get("/api/items", "same")
	require.NoError(t, err)
	require.True(t, ok)
	require.IsType(t, json.RawMessage{}, immediate)
	assert.JSONEq(t, `{"value":1}`, string(immediate.(json.RawMessage)))
	fullKey, enabled, err := manager.cacheKey("/api/items", "same")
	require.NoError(t, err)
	require.True(t, enabled)
	manager.l1.Delete(fullKey)

	value, ok, err := manager.Get("/api/items", "same")
	require.NoError(t, err)
	require.True(t, ok)
	assert.JSONEq(t, `{"value":1}`, string(value.(json.RawMessage)))
	require.NoError(t, manager.l2.Close())

	value, ok, err = manager.Get("/api/items", "same")
	require.NoError(t, err)
	require.True(t, ok)
	assert.JSONEq(t, `{"value":1}`, string(value.(json.RawMessage)))
}

func TestBadgerL2CloseHonorsContextFreeLifecycle(t *testing.T) {
	l2, err := NewBadgerL2(testBadgerL2Config(t.TempDir()))
	require.NoError(t, err)
	require.NoError(t, l2.Close())
	require.NoError(t, l2.Close())
	assert.Error(t, l2.Set("closed", json.RawMessage(`1`), time.Second))
}
