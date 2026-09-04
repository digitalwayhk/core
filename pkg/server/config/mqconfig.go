package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// MQConfig 消息队列配置。Mode=off 不初始化，Mode=auto 自动检测，Mode=on 强制启用。
type MQConfig struct {
	Mode          string                `json:",optional"` // off | auto | on
	Provider      string                `json:",optional"` // redis-stream | nats-jetstream | kafka | rabbitmq | rocketmq
	Usage         []string              `json:",optional"` // event-stream | transport | websocket | delayed-task
	RequestReply  MQRequestReplyConfig  `json:",optional"`
	Retry         MQRetryConfig         `json:",optional"`
	DeadLetter    MQDeadLetterConfig    `json:",optional"`
	Switch        MQSwitchConfig        `json:",optional"`
	RedisStream   RedisStreamMQConfig   `json:",optional"`
	NATSJetStream NATSJetStreamMQConfig `json:",optional"`
	Kafka         KafkaMQConfig         `json:",optional"`
	RabbitMQ      RabbitMQConfig        `json:",optional"`
	RocketMQ      RocketMQConfig        `json:",optional"`
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
	Brokers        []string        `json:",optional"`
	Prefix         string          `json:",optional"`
	ClientID       string          `json:",optional"`
	ConnectTimeout time.Duration   `json:",optional"`
	TLS            MQTLSConfig     `json:",optional"`
	SASL           KafkaSASLConfig `json:",optional"`
}

// KafkaSASLConfig Kafka SASL 认证配置。
type KafkaSASLConfig struct {
	Mechanism string `json:",optional"` // plain | scram-sha-256 | scram-sha-512
	Username  string `json:",optional"`
	Password  string `json:",optional"`
}

// RabbitMQConfig RabbitMQ 连接配置。
type RabbitMQConfig struct {
	URL            string        `json:",optional"`
	Exchange       string        `json:",optional"`
	QueuePrefix    string        `json:",optional"`
	Prefetch       int           `json:",optional"`
	ConnectTimeout time.Duration `json:",optional"`
	TLS            MQTLSConfig   `json:",optional"`
}

// MQTLSConfig MQ Provider 共用的 TLS 配置。
type MQTLSConfig struct {
	Enable     bool   `json:",optional"`
	CAFile     string `json:",optional"`
	CertFile   string `json:",optional"`
	KeyFile    string `json:",optional"`
	ServerName string `json:",optional"`
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
	if m.Kafka.Prefix == "" {
		m.Kafka.Prefix = "digitalway-core"
	}
	if m.Kafka.ClientID == "" {
		m.Kafka.ClientID = "digitalway-core"
	}
	if m.Kafka.ConnectTimeout == 0 {
		m.Kafka.ConnectTimeout = 10 * time.Second
	}
	if m.RabbitMQ.Exchange == "" {
		m.RabbitMQ.Exchange = "digitalway.core.events"
	}
	if m.RabbitMQ.QueuePrefix == "" {
		m.RabbitMQ.QueuePrefix = "digitalway-core"
	}
	if m.RabbitMQ.Prefetch == 0 {
		m.RabbitMQ.Prefetch = 1
	}
	if m.RabbitMQ.ConnectTimeout == 0 {
		m.RabbitMQ.ConnectTimeout = 10 * time.Second
	}
}

// Validate 校验 MQConfig 中的字段合法性。
func (m *MQConfig) Validate() error {
	switch m.Mode {
	case "off", "auto", "on":
	default:
		return fmt.Errorf("mq.mode=%q is invalid; use off, auto, or on", m.Mode)
	}
	if m.Mode == "off" {
		return nil
	}
	if m.Mode == "on" && len(m.Usage) == 0 {
		return errors.New("mq.usage is required when mq.mode is on")
	}
	for _, usage := range m.Usage {
		if usage != "event-stream" {
			return fmt.Errorf("mq.usage contains unsupported value %q; only event-stream is implemented", usage)
		}
	}
	switch m.Provider {
	case "nats-jetstream":
		if m.Mode == "on" && m.NATSJetStream.URL == "" {
			return errors.New("mq.natsJetStream.url is required when provider=nats-jetstream and mode=on")
		}
	case "kafka":
		if err := validateKafkaMQConfig(m.Kafka); err != nil {
			return err
		}
	case "rabbitmq":
		if err := validateRabbitMQConfig(m.RabbitMQ); err != nil {
			return err
		}
	}
	if m.RequestReply.Enable {
		return fmt.Errorf("mq.requestReply.enable=%t is not implemented; remove it or set it to false", m.RequestReply.Enable)
	}
	if m.Retry.Enable {
		return fmt.Errorf("mq.retry.enable=%t is not implemented; remove it or set it to false", m.Retry.Enable)
	}
	if m.DeadLetter.Enable {
		return fmt.Errorf("mq.deadLetter.enable=%t is not implemented; remove it or set it to false", m.DeadLetter.Enable)
	}
	if m.Switch.AllowDynamicSwitch {
		return fmt.Errorf("mq.switch.allowDynamicSwitch=%t is not implemented; remove it or set it to false", m.Switch.AllowDynamicSwitch)
	}
	// 空字符串表示"未配置，使用 ApplyDefaults 后的默认值"
	switch m.Switch.Strategy {
	case "", "drain", "dual-write", "maintenance":
	default:
		return fmt.Errorf("mq.switch.strategy=%q is invalid; use drain, dual-write, or maintenance", m.Switch.Strategy)
	}
	return nil
}

func validateKafkaMQConfig(cfg KafkaMQConfig) error {
	hasBroker := false
	for _, broker := range cfg.Brokers {
		if strings.TrimSpace(broker) != "" {
			hasBroker = true
			break
		}
	}
	if !hasBroker {
		return errors.New("mq.kafka.brokers requires at least one non-empty broker")
	}
	mechanism := strings.ToLower(strings.TrimSpace(cfg.SASL.Mechanism))
	switch mechanism {
	case "":
		if cfg.SASL.Username != "" || cfg.SASL.Password != "" {
			return errors.New("mq.kafka.sasl.mechanism is required when credentials are configured")
		}
	case "plain", "scram-sha-256", "scram-sha-512":
		if cfg.SASL.Username == "" {
			return errors.New("mq.kafka.sasl.username is required when mechanism is configured")
		}
		if cfg.SASL.Password == "" {
			return errors.New("mq.kafka.sasl.password is required when mechanism is configured")
		}
	default:
		return errors.New("mq.kafka.sasl.mechanism is invalid; use plain, scram-sha-256, or scram-sha-512")
	}
	return validateMQTLSConfig("mq.kafka.tls", cfg.TLS)
}

func validateRabbitMQConfig(cfg RabbitMQConfig) error {
	if strings.TrimSpace(cfg.URL) == "" {
		return errors.New("mq.rabbitMQ.url is required when provider=rabbitmq")
	}
	parsed, err := url.Parse(cfg.URL)
	if err != nil || parsed.Scheme == "" {
		return errors.New("mq.rabbitMQ.url is invalid")
	}
	if parsed.Scheme != "amqp" && parsed.Scheme != "amqps" {
		return errors.New("mq.rabbitMQ.url scheme must be amqp or amqps")
	}
	if strings.TrimSpace(cfg.Exchange) == "" {
		return errors.New("mq.rabbitMQ.exchange is required when provider=rabbitmq")
	}
	if cfg.Prefetch <= 0 {
		return errors.New("mq.rabbitMQ.prefetch must be greater than zero")
	}
	return validateMQTLSConfig("mq.rabbitMQ.tls", cfg.TLS)
}

func validateMQTLSConfig(path string, cfg MQTLSConfig) error {
	if (cfg.CertFile == "") != (cfg.KeyFile == "") {
		return fmt.Errorf("%s.certFile and %s.keyFile must be configured together", path, path)
	}
	return nil
}
