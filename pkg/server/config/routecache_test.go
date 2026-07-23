package config_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteCacheL1ConfigExposesWeightedLimits(t *testing.T) {
	typeOfConfig := reflect.TypeOf(config.RouteCacheL1Config{})
	for _, field := range []string{"MaxEntries", "MaxValueBytes", "MaxBytes"} {
		_, ok := typeOfConfig.FieldByName(field)
		assert.Truef(t, ok, "RouteCacheL1Config 缺少字段 %s", field)
	}
	_, legacyLimitExists := typeOfConfig.FieldByName("Limit")
	assert.True(t, legacyLimitExists, "Limit 必须在迁移窗口内保留兼容")
}

func TestRouteCacheConfigDefaultsToLocal(t *testing.T) {
	cfg := config.RouteCacheConfig{}
	cfg.ApplyDefaults()

	assert.Equal(t, "local", cfg.Mode)
	assert.Equal(t, 10*time.Second, cfg.TTL)
	assert.Zero(t, cfg.L1.MaxEntries, "MaxEntries=0 表示运行时自动解析")
	assert.Greater(t, cfg.L1.MaxValueBytes, int64(0))
	require.NoError(t, cfg.Validate())
}

func TestRouteCacheLegacyOffNormalizesToLocal(t *testing.T) {
	cfg := config.RouteCacheConfig{Mode: "off"}
	cfg.ApplyDefaults()

	assert.Equal(t, "local", cfg.Mode)
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
