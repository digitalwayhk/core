package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMQConfigApplyDefaults_EmptyStruct 验证零值 MQConfig 补充后所有默认值正确。
func TestMQConfigApplyDefaults_EmptyStruct(t *testing.T) {
	var m MQConfig
	m.ApplyDefaults()

	assert.Equal(t, "auto", m.Mode)
	assert.Equal(t, "redis-stream", m.Provider)
	assert.Equal(t, 3, m.Retry.RetryCount)
	assert.Equal(t, 100*time.Millisecond, m.Retry.InitialDelay)
	assert.Equal(t, 5*time.Second, m.Retry.MaxDelay)
	assert.Equal(t, "digitalway.core.deadletter", m.DeadLetter.Topic)
	assert.Equal(t, "digitalway-core", m.RedisStream.Prefix)
	assert.Equal(t, "digitalway-core", m.NATSJetStream.StreamPrefix)
	assert.Equal(t, "core", m.NATSJetStream.DurablePrefix)
	assert.Equal(t, "dual-write", m.Switch.Strategy)
	assert.Equal(t, 30*time.Second, m.Switch.DualWriteDuration)
}

// TestMQConfigApplyDefaults_PreserveExistingValues 验证已设置的值不被覆盖。
func TestMQConfigApplyDefaults_PreserveExistingValues(t *testing.T) {
	m := MQConfig{Mode: "on", Provider: "nats-jetstream"}
	m.ApplyDefaults()

	assert.Equal(t, "on", m.Mode)
	assert.Equal(t, "nats-jetstream", m.Provider)
	// 未设置的字段应补默认值
	assert.Equal(t, 3, m.Retry.RetryCount)
}

// TestMQConfigApplyDefaults_OldJSON 模拟旧配置不含 MQ 字段时解析后补默认值不 panic。
func TestMQConfigApplyDefaults_OldJSON(t *testing.T) {
	oldJSON := `{}`
	var m MQConfig
	require.NoError(t, json.Unmarshal([]byte(oldJSON), &m))
	assert.NotPanics(t, func() { m.ApplyDefaults() })
	assert.Equal(t, "auto", m.Mode)
	assert.Equal(t, "redis-stream", m.Provider)
}

// TestMQConfigValidate_ValidModes 合法 mode 不报错。
func TestMQConfigValidate_ValidModes(t *testing.T) {
	for _, mode := range []string{"off", "auto", "on"} {
		m := MQConfig{Mode: mode, Provider: "redis-stream"}
		if mode == "on" {
			m.Usage = []string{"event-stream"}
		}
		assert.NoError(t, m.Validate(), "mode=%s", mode)
	}
}

// TestMQConfigValidate_InvalidMode 非法 mode 返回 error。
func TestMQConfigValidate_InvalidMode(t *testing.T) {
	m := MQConfig{Mode: "enabled", Provider: "redis-stream"}
	assert.Error(t, m.Validate())
}

// TestMQConfigValidate_InvalidProvider 非法 provider 返回 error。
func TestMQConfigValidate_InvalidProvider(t *testing.T) {
	m := MQConfig{Mode: "auto", Provider: "pulsar"}
	assert.Error(t, m.Validate())
}

// TestMQConfigValidate_NATSRequiresURL Mode=on + Provider=nats-jetstream 但无 URL 时报错。
func TestMQConfigValidate_NATSRequiresURL(t *testing.T) {
	m := MQConfig{Mode: "on", Provider: "nats-jetstream", Usage: []string{"event-stream"}}
	assert.Error(t, m.Validate())

	m.NATSJetStream.URL = "nats://127.0.0.1:4222"
	assert.NoError(t, m.Validate())
}

// TestMQConfigValidate_KafkaRequiresBrokers Mode=on + Provider=kafka 但无 Brokers 时报错。
func TestMQConfigValidate_KafkaRequiresBrokers(t *testing.T) {
	m := MQConfig{Mode: "on", Provider: "kafka", Usage: []string{"event-stream"}}
	assert.Error(t, m.Validate())

	m.Kafka.Brokers = []string{"127.0.0.1:9092"}
	assert.NoError(t, m.Validate())
}

// TestMQConfigValidate_RabbitMQRequiresURL Mode=on + Provider=rabbitmq 但无 URL 时报错。
func TestMQConfigValidate_RabbitMQRequiresURL(t *testing.T) {
	m := MQConfig{Mode: "on", Provider: "rabbitmq", Usage: []string{"event-stream"}}
	assert.Error(t, m.Validate())

	m.RabbitMQ.URL = "amqp://guest:guest@127.0.0.1:5672/"
	assert.NoError(t, m.Validate())
}

// TestMQConfigValidate_RocketMQRequiresNameServers Mode=on + Provider=rocketmq 但无 NameServers 时报错。
func TestMQConfigValidate_RocketMQRequiresNameServers(t *testing.T) {
	m := MQConfig{Mode: "on", Provider: "rocketmq", Usage: []string{"event-stream"}}
	assert.Error(t, m.Validate())

	m.RocketMQ.NameServers = []string{"127.0.0.1:9876"}
	assert.NoError(t, m.Validate())
}

// TestMQConfigSwitchConfig_RollbackOnFailure_WhenDynamicSwitchEnabled
// 验证 AllowDynamicSwitch=true 时 RollbackOnFailure 默认为 true 指针。
func TestMQConfigSwitchConfig_RollbackOnFailure_WhenDynamicSwitchEnabled(t *testing.T) {
	m := MQConfig{
		Switch: MQSwitchConfig{AllowDynamicSwitch: true},
	}
	m.ApplyDefaults()
	require.NotNil(t, m.Switch.RollbackOnFailure)
	assert.True(t, *m.Switch.RollbackOnFailure)
}

// TestMQConfigSwitchConfig_RollbackOnFailure_WhenDynamicSwitchDisabled
// 验证 AllowDynamicSwitch=false 时 RollbackOnFailure 不被设置。
func TestMQConfigSwitchConfig_RollbackOnFailure_WhenDynamicSwitchDisabled(t *testing.T) {
	var m MQConfig
	m.ApplyDefaults()
	assert.Nil(t, m.Switch.RollbackOnFailure)
}

// TestMQConfigValidate_SwitchInvalidStrategy Switch.Strategy 非法时报错。
func TestMQConfigValidate_SwitchInvalidStrategy(t *testing.T) {
	m := MQConfig{
		Mode:     "auto",
		Provider: "redis-stream",
		Switch: MQSwitchConfig{
			Strategy: "bad-strategy",
		},
	}
	assert.Error(t, m.Validate())
}

// TestMQConfigValidate_SwitchValidStrategies 合法 strategy 不报错。
func TestMQConfigValidate_SwitchValidStrategies(t *testing.T) {
	for _, strategy := range []string{"drain", "dual-write", "maintenance"} {
		m := MQConfig{
			Mode:     "auto",
			Provider: "redis-stream",
			Switch: MQSwitchConfig{
				Strategy: strategy,
			},
		}
		assert.NoError(t, m.Validate(), "strategy=%s", strategy)
	}
}

