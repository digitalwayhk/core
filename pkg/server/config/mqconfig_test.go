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

// TestMQConfigApplyDefaults_KafkaAndRabbitMQ 验证两个内建 MQ Provider 的安全默认值。
func TestMQConfigApplyDefaults_KafkaAndRabbitMQ(t *testing.T) {
	var m MQConfig
	m.ApplyDefaults()

	assert.Equal(t, "digitalway-core", m.Kafka.Prefix)
	assert.Equal(t, "digitalway-core", m.Kafka.ClientID)
	assert.Equal(t, 10*time.Second, m.Kafka.ConnectTimeout)
	assert.Equal(t, "digitalway.core.events", m.RabbitMQ.Exchange)
	assert.Equal(t, "digitalway-core", m.RabbitMQ.QueuePrefix)
	assert.Equal(t, 1, m.RabbitMQ.Prefetch)
	assert.Equal(t, 10*time.Second, m.RabbitMQ.ConnectTimeout)
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

// TestMQConfigValidate_CustomProvider 自定义 provider 留给已注册 factory 解析。
func TestMQConfigValidate_CustomProvider(t *testing.T) {
	m := MQConfig{Mode: "auto", Provider: "pulsar"}
	assert.NoError(t, m.Validate())
}

// TestMQConfigValidate_NATSRequiresURL Mode=on + Provider=nats-jetstream 但无 URL 时报错。
func TestMQConfigValidate_NATSRequiresURL(t *testing.T) {
	m := MQConfig{Mode: "on", Provider: "nats-jetstream", Usage: []string{"event-stream"}}
	assert.Error(t, m.Validate())

	m.NATSJetStream.URL = "nats://127.0.0.1:4222"
	assert.NoError(t, m.Validate())
}

// TestMQConfigValidate_Kafka 验证 Kafka 必填项、SASL 和 TLS 配置 fail closed。
func TestMQConfigValidate_Kafka(t *testing.T) {
	valid := MQConfig{
		Mode:     "on",
		Provider: "kafka",
		Usage:    []string{"event-stream"},
		Kafka: KafkaMQConfig{
			Brokers: []string{"127.0.0.1:9092"},
		},
	}
	valid.ApplyDefaults()
	require.NoError(t, valid.Validate())

	tests := []struct {
		name      string
		configure func(*MQConfig)
		want      string
	}{
		{name: "empty broker", configure: func(m *MQConfig) { m.Kafka.Brokers = []string{"", "  "} }, want: "brokers"},
		{name: "unknown sasl", configure: func(m *MQConfig) { m.Kafka.SASL.Mechanism = "oauth" }, want: "mechanism"},
		{name: "sasl missing username", configure: func(m *MQConfig) {
			m.Kafka.SASL = KafkaSASLConfig{Mechanism: "plain", Password: "secret"}
		}, want: "username"},
		{name: "sasl missing password", configure: func(m *MQConfig) {
			m.Kafka.SASL = KafkaSASLConfig{Mechanism: "scram-sha-256", Username: "alice"}
		}, want: "password"},
		{name: "tls missing key", configure: func(m *MQConfig) {
			m.Kafka.TLS = MQTLSConfig{Enable: true, CertFile: "client.crt"}
		}, want: "certFile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.configure(&cfg)
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// TestMQConfigValidate_RabbitMQ 验证 RabbitMQ URL、exchange、prefetch 和 TLS 配置。
func TestMQConfigValidate_RabbitMQ(t *testing.T) {
	valid := MQConfig{
		Mode:     "on",
		Provider: "rabbitmq",
		Usage:    []string{"event-stream"},
		RabbitMQ: RabbitMQConfig{
			URL:      "amqp://guest:guest@127.0.0.1:5672/",
			Exchange: "events",
			Prefetch: 1,
		},
	}
	valid.ApplyDefaults()
	require.NoError(t, valid.Validate())

	tests := []struct {
		name      string
		configure func(*MQConfig)
		want      string
	}{
		{name: "empty url", configure: func(m *MQConfig) { m.RabbitMQ.URL = "" }, want: "url"},
		{name: "bad scheme", configure: func(m *MQConfig) { m.RabbitMQ.URL = "https://example.com" }, want: "scheme"},
		{name: "empty exchange", configure: func(m *MQConfig) { m.RabbitMQ.Exchange = "" }, want: "exchange"},
		{name: "zero prefetch", configure: func(m *MQConfig) { m.RabbitMQ.Prefetch = 0 }, want: "prefetch"},
		{name: "tls missing cert", configure: func(m *MQConfig) {
			m.RabbitMQ.TLS = MQTLSConfig{Enable: true, KeyFile: "client.key"}
		}, want: "certFile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.configure(&cfg)
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// TestMQConfigValidate_KafkaAndRabbitMQAutoStillRequireConnectionConfig 验证 auto 仅允许连接失败降级，不允许缺配置。
func TestMQConfigValidate_KafkaAndRabbitMQAutoStillRequireConnectionConfig(t *testing.T) {
	for _, provider := range []string{"kafka", "rabbitmq"} {
		t.Run(provider, func(t *testing.T) {
			cfg := MQConfig{Mode: "auto", Provider: provider, Usage: []string{"event-stream"}}
			cfg.ApplyDefaults()
			require.Error(t, cfg.Validate())
		})
	}
}

func TestMQConfigValidate_RocketMQConstructionRemainsFactoryResponsibility(t *testing.T) {
	err := (&MQConfig{Mode: "auto", Provider: "rocketmq", Usage: []string{"event-stream"}}).Validate()
	assert.NoError(t, err, "RocketMQ 是否可构建仍由已注册 factory 或 BuildManager 决定")
}

func TestMQConfigValidate_UnsupportedUsage(t *testing.T) {
	for _, usage := range []string{"unknown", "transport", "websocket", "delayed-task"} {
		t.Run(usage, func(t *testing.T) {
			err := (&MQConfig{Mode: "auto", Provider: "redis-stream", Usage: []string{usage}}).Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "mq.usage")
		})
	}
}

func TestMQConfigValidate_UnimplementedCapabilities(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*MQConfig)
		fieldPath string
	}{
		{name: "request reply", configure: func(m *MQConfig) { m.RequestReply.Enable = true }, fieldPath: "mq.requestReply.enable"},
		{name: "retry", configure: func(m *MQConfig) { m.Retry.Enable = true }, fieldPath: "mq.retry.enable"},
		{name: "dead letter", configure: func(m *MQConfig) { m.DeadLetter.Enable = true }, fieldPath: "mq.deadLetter.enable"},
		{name: "dynamic switch", configure: func(m *MQConfig) { m.Switch.AllowDynamicSwitch = true }, fieldPath: "mq.switch.allowDynamicSwitch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := MQConfig{Mode: "auto", Provider: "redis-stream", Usage: []string{"event-stream"}}
			tt.configure(&m)
			err := m.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.fieldPath)
			assert.Contains(t, err.Error(), "not implemented")
		})
	}
}

func TestMQConfigValidate_ModeOffAllowsLegacyFields(t *testing.T) {
	m := MQConfig{
		Mode:         "off",
		Provider:     "kafka",
		Usage:        []string{"transport", "websocket", "delayed-task"},
		RequestReply: MQRequestReplyConfig{Enable: true},
		Retry:        MQRetryConfig{Enable: true},
		DeadLetter:   MQDeadLetterConfig{Enable: true},
		Switch:       MQSwitchConfig{AllowDynamicSwitch: true, Strategy: "legacy"},
	}
	assert.NoError(t, m.Validate())
}

func TestMQConfigValidate_RejectedErrorsIncludeValue(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*MQConfig)
		want      string
	}{
		{name: "mode", configure: func(m *MQConfig) { m.Mode = "legacy" }, want: `"legacy"`},
		{name: "usage", configure: func(m *MQConfig) { m.Usage = []string{"transport"} }, want: `"transport"`},
		{name: "request reply", configure: func(m *MQConfig) { m.RequestReply.Enable = true }, want: "true"},
		{name: "retry", configure: func(m *MQConfig) { m.Retry.Enable = true }, want: "true"},
		{name: "dead letter", configure: func(m *MQConfig) { m.DeadLetter.Enable = true }, want: "true"},
		{name: "dynamic switch", configure: func(m *MQConfig) { m.Switch.AllowDynamicSwitch = true }, want: "true"},
		{name: "strategy", configure: func(m *MQConfig) { m.Switch.Strategy = "legacy" }, want: `"legacy"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg MQConfig
			cfg.ApplyDefaults()
			tt.configure(&cfg)
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
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
