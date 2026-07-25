package routecache

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTTLJitterStaysWithinTenPercentAndVaries(t *testing.T) {
	const base = time.Minute
	lower := base - base/10
	upper := base + base/10
	seen := make(map[time.Duration]struct{})
	for range 256 {
		value := jitterTTL(base)
		assert.GreaterOrEqual(t, value, lower)
		assert.LessOrEqual(t, value, upper)
		seen[value] = struct{}{}
	}
	assert.Greater(t, len(seen), 1)
}

func TestManagerSetUsesEffectiveJitteredTTL(t *testing.T) {
	cfg := config.RouteCacheConfig{
		Mode: "local",
		TTL:  time.Minute,
		L1:   config.RouteCacheL1Config{Limit: 16},
	}
	cfg.ApplyDefaults()
	manager, err := NewManager("service-a", cfg)
	require.NoError(t, err)
	t.Cleanup(manager.Close)
	require.NoError(t, manager.EnableRoute("/api/items", cfg.TTL))

	const effective = 54 * time.Second
	manager.ttlJitter = func(time.Duration) time.Duration { return effective }
	before := time.Now()
	require.NoError(t, manager.Set("/api/items", "same", "value", cfg.TTL))
	key, enabled, err := manager.cacheKey("/api/items", "same")
	require.NoError(t, err)
	require.True(t, enabled)
	stored, ok := manager.l1.cache.Get(key)
	require.True(t, ok)
	entry := stored.(l1Entry)
	assert.GreaterOrEqual(t, entry.expiresAt, before.Add(effective))
	assert.Less(t, entry.expiresAt, before.Add(effective+time.Second))
}
