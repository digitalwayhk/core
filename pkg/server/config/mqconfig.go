package config

import (
	"errors"
	"time"
)

// MQConfig 消息队列配置。Mode=off 不初始化，Mode=auto 自动检测，Mode=on 强制启用。
type MQConfig struct {
	Mode          string              `json:",optional"` // off | auto | on
	Provider      string              `json:",optional"` // redis-stream | nats-jetstream | kafka | rabbitmq | rocketmq
	Usage         []string            `json:",optional"` // event-stream | transport | websocket | delayed-task
	RequestReply  MQRequestReplyConfig `json:",optional"`
	Retry         MQRetryConfig        `json:",optional"`
	DeadLetter    MQDeadLetterConfig   `json:",optional"`
	Switch        MQSwitchConfig       `json:",optional"`
	RedisStream   RedisStreamMQConfig  `json:",optional"`
	NATSJetStream NATSJetStreamMQConfig `json:",optional"`
	Kafka         KafkaMQConfig        `json:",optional"`
	RabbitMQ      RabbitMQConfig       `json:",optional"`
	RocketMQ      RocketMQConfig       `json:",optional"`
}

// MQRequestReplyConfig MQ 同步 request/reply 配置。
type MQRequestReplyConfig struct {
	Enable  bool          `json:",optional"`
	Timeout time.Duration `json:",optional"`
}

// MQRetryConfig MQ 消息重试配置。
type MQRetryConfig struct {
	Enable       bool          `json:",optional"`
	RetryCount   int           `json:",optional"`
	InitialDelay time.Duration `json:",optional"`
	MaxDelay     time.Duration `json:",optional"`
}

// MQDeadLetterConfig 死信队列配置。
type MQDeadLetterConfig struct {
	Enable bool   `json:",optional"`
	Topic  string `json:",optional"`
}

// MQSwitchConfig MQ 动态切换配置。
type MQSwitchConfig struct {
	AllowDynamicSwitch bool          `json:",optional"`
	Strategy           string        `json:",optional"` // drain | dual-write | maintenance
	TargetProvider     string        `json:",optional"`
	DualWriteDuration  time.Duration `json:",optional"`
	// RollbackOnFailure 开启动态切换时默认 true。使用指针以区分"未设置"和"显式 false"。
	RollbackOnFailure *bool `json:",optional"`
}

// RedisStreamMQConfig Redis Streams 连接配置。
type RedisStreamMQConfig struct {
	Addr   string `json:",optional"`
	DB     int    `json:",optional"`
	Prefix string `json:",optional"`
}

// NATSJetStreamMQConfig NATS JetStream 连接配置。
type NATSJetStreamMQConfig struct {
	URL           string `json:",optional"`
	StreamPrefix  string `json:",optional"`
	DurablePrefix string `json:",optional"`
}

// KafkaMQConfig Kafka 连接配置。
type KafkaMQConfig struct {
	Brokers []string `json:",optional"`
	Prefix  string   `json:",optional"`
}

// RabbitMQConfig RabbitMQ 连接配置。
type RabbitMQConfig struct {
	URL      string `json:",optional"`
	Exchange string `json:",optional"`
}

// RocketMQConfig RocketMQ 连接配置。
type RocketMQConfig struct {
	NameServers []string `json:",optional"`
	Group       string   `json:",optional"`
}

// ApplyDefaults 为 MQConfig 补充缺失的默认值。
func (m *MQConfig) ApplyDefaults() {
	if m.Mode == "" {
		m.Mode = "auto"
	}
	if m.Provider == "" {
		m.Provider = "redis-stream"
	}
	if len(m.Usage) == 0 {
		m.Usage = []string{"event-stream"}
	}
	if m.RequestReply.Timeout == 0 {
		m.RequestReply.Timeout = 5 * time.Second
	}
	if m.Retry.RetryCount == 0 {
		m.Retry.RetryCount = 3
	}
	if m.Retry.InitialDelay == 0 {
		m.Retry.InitialDelay = 100 * time.Millisecond
	}
	if m.Retry.MaxDelay == 0 {
		m.Retry.MaxDelay = 5 * time.Second
	}
	if m.DeadLetter.Topic == "" {
		m.DeadLetter.Topic = "digitalway.core.deadletter"
	}
	if m.Switch.Strategy == "" {
		m.Switch.Strategy = "dual-write"
	}
	if m.Switch.DualWriteDuration == 0 {
		m.Switch.DualWriteDuration = 30 * time.Second
	}
	// 开启动态切换时，RollbackOnFailure 默认为 true
	if m.Switch.AllowDynamicSwitch && m.Switch.RollbackOnFailure == nil {
		t := true
		m.Switch.RollbackOnFailure = &t
	}
	if m.RedisStream.Prefix == "" {
		m.RedisStream.Prefix = "digitalway-core"
	}
	if m.NATSJetStream.StreamPrefix == "" {
		m.NATSJetStream.StreamPrefix = "digitalway-core"
	}
	if m.NATSJetStream.DurablePrefix == "" {
		m.NATSJetStream.DurablePrefix = "core"
	}
}

// Validate 校验 MQConfig 中的字段合法性。
func (m *MQConfig) Validate() error {
	switch m.Mode {
	case "off", "auto", "on":
	default:
		return errors.New("mq.mode must be off, auto, or on")
	}
	switch m.Provider {
	case "redis-stream", "nats-jetstream", "kafka", "rabbitmq", "rocketmq":
	default:
		return errors.New("mq.provider must be one of: redis-stream, nats-jetstream, kafka, rabbitmq, rocketmq")
	}
	if m.Mode == "on" && len(m.Usage) == 0 {
		return errors.New("mq.usage is required when mq.mode is on")
	}
	if m.Mode == "on" {
		switch m.Provider {
		case "nats-jetstream":
			if m.NATSJetStream.URL == "" {
				return errors.New("mq.natsJetStream.url is required when provider=nats-jetstream and mode=on")
			}
		case "kafka":
			if len(m.Kafka.Brokers) == 0 {
				return errors.New("mq.kafka.brokers is required when provider=kafka and mode=on")
			}
		case "rabbitmq":
			if m.RabbitMQ.URL == "" {
				return errors.New("mq.rabbitMQ.url is required when provider=rabbitmq and mode=on")
			}
		case "rocketmq":
			if len(m.RocketMQ.NameServers) == 0 {
				return errors.New("mq.rocketMQ.nameServers is required when provider=rocketmq and mode=on")
			}
		}
	}
	if m.RequestReply.Enable && m.RequestReply.Timeout <= 0 {
		return errors.New("mq.requestReply.timeout must be greater than zero when requestReply is enabled")
	}
	// 空字符串表示"未配置，使用 ApplyDefaults 后的默认值"
	switch m.Switch.Strategy {
	case "", "drain", "dual-write", "maintenance":
	default:
		return errors.New("mq.switch.strategy must be drain, dual-write, or maintenance")
	}
	return nil
}
