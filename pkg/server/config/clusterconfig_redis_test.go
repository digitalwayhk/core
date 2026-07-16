package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterConfigApplyDefaults_RedisProvider(t *testing.T) {
	cfg := ClusterConfig{Mode: "on", Provider: "redis"}
	cfg.ApplyDefaults()

	assert.Equal(t, DefaultClusterRedisPrefix, cfg.Providers.Redis.Prefix)
	assert.Equal(t, DefaultClusterProviderTTL, cfg.Providers.Redis.TTL)
}

func TestClusterConfigValidate_RedisRequiresAddressWhenEnabled(t *testing.T) {
	cfg := ClusterConfig{Mode: "on", Provider: "redis"}
	cfg.ApplyDefaults()

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster.providers.redis.addr")

	cfg.Providers.Redis.Addr = "127.0.0.1:6379"
	require.NoError(t, cfg.Validate())
}

func TestClusterConfigValidate_AdvertiseAddressIsSupported(t *testing.T) {
	cfg := ClusterConfig{
		Mode:             "on",
		Provider:         "redis",
		AdvertiseAddress: "order-service",
		Providers: ClusterProviderConfig{
			Redis: RedisProviderConfig{
				Addr:   "127.0.0.1:6379",
				Prefix: "core:test:discovery",
				TTL:    10 * time.Second,
			},
		},
	}

	require.NoError(t, cfg.Validate())
}
