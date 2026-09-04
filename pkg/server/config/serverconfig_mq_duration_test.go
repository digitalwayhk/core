package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrateDurations_ConvertsMQConnectTimeout 验证直接 JSON/旧配置中的 MQ 连接超时数字会在加载前迁移为 duration 字符串。
func TestMigrateDurations_ConvertsMQConnectTimeout(t *testing.T) {
	configMap := map[string]interface{}{
		"MQ": map[string]interface{}{
			"Kafka": map[string]interface{}{
				"ConnectTimeout": float64((10 * time.Second).Nanoseconds()),
			},
			"RabbitMQ": map[string]interface{}{
				"ConnectTimeout": float64((5 * time.Second).Nanoseconds()),
			},
		},
	}

	require.True(t, migrateDurations(configMap))
	mqConfig := configMap["MQ"].(map[string]interface{})
	assert.Equal(t, "10s", mqConfig["Kafka"].(map[string]interface{})["ConnectTimeout"])
	assert.Equal(t, "5s", mqConfig["RabbitMQ"].(map[string]interface{})["ConnectTimeout"])
}
