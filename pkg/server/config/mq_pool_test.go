package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMQRedisPoolBudgetValidation(t *testing.T) {
	for _, field := range []string{"CommandPoolSize", "BlockingReadPoolSize"} {
		t.Run(field, func(t *testing.T) {
			var cfg MQConfig
			require.NoError(t, json.Unmarshal([]byte(`{"Mode":"on","RedisStream":{"`+field+`":-1}}`), &cfg))
			cfg.ApplyDefaults()
			require.Error(t, cfg.Validate(), "不能接受负数连接预算")
		})
	}
}
