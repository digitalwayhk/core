package config_test

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteCacheConfigDefaultsToOff(t *testing.T) {
	cfg := config.RouteCacheConfig{}
	cfg.ApplyDefaults()

	assert.Equal(t, "off", cfg.Mode)
	assert.Equal(t, 10*time.Second, cfg.TTL)
	assert.Greater(t, cfg.L1.Limit, 0)
	require.NoError(t, cfg.Validate())
}

func TestRouteCacheSharedModeAcceptsRedisDatabaseZero(t *testing.T) {
	cfg := config.RouteCacheConfig{
		Mode: "shared",
		Redis: config.RouteCacheRedisConfig{
			Addr: "127.0.0.1:6379",
			DB:   0,
		},
	}
	cfg.ApplyDefaults()

	require.NoError(t, cfg.Validate())
}

func TestRouteCacheRejectsUnsupportedRedisDatabase(t *testing.T) {
	cfg := config.RouteCacheConfig{
		Mode: "shared",
		Redis: config.RouteCacheRedisConfig{
			Addr: "127.0.0.1:6379",
			DB:   1,
		},
	}
	cfg.ApplyDefaults()

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database 0")
}
