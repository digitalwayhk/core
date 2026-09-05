package mq

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"
	"github.com/zeromicro/go-zero/core/logx"
)

var _ MQProvider = (*KafkaProvider)(nil)
var _ ReliableMQProvider = (*KafkaProvider)(nil)

type kafkaMessageWriter interface {
	WriteMessages(ctx context.Context, messages ...kafka.Message) error
	Close() error
}

type kafkaMessageReader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, messages ...kafka.Message) error
	Close() error
}

type kafkaSubscription struct {
	cancel context.CancelFunc
	done   chan struct{}

	readerMu sync.Mutex
	reader   kafkaMessageReader
}

func (s *kafkaSubscription) setReader(reader kafkaMessageReader) {
	s.readerMu.Lock()
	old := s.reader
	s.reader = reader
	s.readerMu.Unlock()
	if old != nil && old != reader {
		_ = old.Close()
	}
}

func (s *kafkaSubscription) closeReader() {
	s.readerMu.Lock()
	reader := s.reader
	s.reader = nil
	s.readerMu.Unlock()
	if reader != nil {
		_ = reader.Close()
	}
}

// KafkaProvider 使用 kafka-go 实现普通发布订阅与 at-least-once 可靠消费。
type KafkaProvider struct {
	cfg config.KafkaMQConfig

	stateMu       sync.RWMutex
	connected     bool
	closed        bool
	dialer        *kafka.Dialer
	transport     *kafka.Transport
	writer        kafkaMessageWriter
	topicAdmin    kafkaTopicAdmin
	readerFactory func(kafka.ReaderConfig) kafkaMessageReader
	subscriptions map[uint64]*kafkaSubscription
	nextSubID     uint64

	closeOnce sync.Once
	closeErr  error
	wg        sync.WaitGroup
}

// NewKafkaProvider 创建尚未连接的 Kafka Provider。
func NewKafkaProvider(cfg config.KafkaMQConfig) *KafkaProvider {
	if cfg.Prefix == "" {
		cfg.Prefix = "digitalway-core"
	}
	if cfg.ClientID == "" {
		cfg.ClientID = "digitalway-core"
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 10 * time.Second
	}
	if cfg.StartOffset == "" {
		cfg.StartOffset = "latest"
	}
	return &KafkaProvider{
		cfg:           cfg,
		readerFactory: func(readerConfig kafka.ReaderConfig) kafkaMessageReader { return kafka.NewReader(readerConfig) },
		subscriptions: make(map[uint64]*kafkaSubscription),
	}
}

// Name 返回 Kafka Provider 的稳定标识。
func (*KafkaProvider) Name() string { return "kafka" }

// Connect 装配 TLS/SASL 并通过 metadata 请求验证 Broker 可达。
func (p *KafkaProvider) Connect(ctx context.Context) error {
	p.stateMu.RLock()
	if p.closed {
		p.stateMu.RUnlock()
		return ErrNotConnected
	}
	if p.connected {
		p.stateMu.RUnlock()
		return nil
	}
	p.stateMu.RUnlock()

	if err := validateKafkaProviderConfig(p.cfg); err != nil {
		return fmt.Errorf("%w: %v", ErrProviderConfiguration, err)
	}
	tlsConfig, err := buildMQTLSConfig(p.cfg.TLS)
	if err != nil {
		return fmt.Errorf("%w: kafka TLS: %v", ErrProviderConfiguration, err)
	}
	mechanism, err := kafkaSASLMechanism(p.cfg.SASL)
	if err != nil {
		return fmt.Errorf("%w: kafka SASL: %v", ErrProviderConfiguration, err)
	}
	dialer := &kafka.Dialer{
		ClientID:      p.cfg.ClientID,
		Timeout:       p.cfg.ConnectTimeout,
		DualStack:     true,
		TLS:           tlsConfig,
		SASLMechanism: mechanism,
	}
	checkCtx, cancel := boundedContext(ctx, p.cfg.ConnectTimeout)
	defer cancel()
	connection, err := dialer.DialContext(checkCtx, "tcp", firstKafkaBroker(p.cfg.Brokers))
	if err != nil {
		return fmt.Errorf("kafka: metadata connection failed: %w", err)
	}
	_, metadataErr := connection.Brokers()
	closeErr := connection.Close()
	if metadataErr != nil {
		return fmt.Errorf("kafka: metadata request failed: %w", metadataErr)
	}
	if closeErr != nil {
		return fmt.Errorf("kafka: close metadata connection: %w", closeErr)
	}

	transport := &kafka.Transport{
		DialTimeout: p.cfg.ConnectTimeout,
		ClientID:    p.cfg.ClientID,
		TLS:         tlsConfig,
		SASL:        mechanism,
	}
	writer := &kafka.Writer{
		Addr:                   kafka.TCP(nonEmptyKafkaBrokers(p.cfg.Brokers)...),
		Balancer:               &kafka.Murmur2Balancer{},
		RequiredAcks:           kafka.RequireAll,
		Async:                  false,
		AllowAutoTopicCreation: true,
		Transport:              transport,
		ReadTimeout:            p.cfg.ConnectTimeout,
		WriteTimeout:           p.cfg.ConnectTimeout,
	}

	p.stateMu.Lock()
	if p.closed {
		p.stateMu.Unlock()
		_ = writer.Close()
		transport.CloseIdleConnections()
		return ErrNotConnected
	}
	p.dialer = dialer
	p.transport = transport
	p.writer = writer
	p.topicAdmin = &kafkaGoTopicAdmin{
		created: make(map[string]struct{}),
		client: &kafka.Client{
			Addr:      kafka.TCP(nonEmptyKafkaBrokers(p.cfg.Brokers)...),
			Timeout:   p.cfg.ConnectTimeout,
			Transport: transport,
		},
	}
	p.connected = true
	p.stateMu.Unlock()
	return nil
}

// Publish 同步等待 Kafka 全部 ISR 确认后返回。
func (p *KafkaProvider) Publish(ctx context.Context, subject string, data []byte, opts *PublishOptions) error {
	p.stateMu.RLock()
	if !p.connected || p.closed || p.writer == nil {
		p.stateMu.RUnlock()
		return ErrNotConnected
	}
	writer := p.writer
	prefix := p.cfg.Prefix
	p.stateMu.RUnlock()

	topic := mqResourceName(prefix, subject, 249)
	if err := p.ensureTopics(ctx, topic); err != nil {
		return fmt.Errorf("kafka: ensure topic: %w", err)
	}
	message := kafka.Message{
		Topic: topic,
		Value: data,
	}
	if opts != nil {
		if opts.OrderingKey != "" {
			message.Key = []byte(opts.OrderingKey)
		}
		if opts.IdempotencyKey != "" {
			message.Headers = []kafka.Header{{Key: "core-idempotency-key", Value: []byte(opts.IdempotencyKey)}}
		}
	}
	if err := writer.WriteMessages(ctx, message); err != nil {
		return fmt.Errorf("kafka: publish: %w", err)
	}
	return nil
}

// Subscribe 使用 Provider 默认消费组，并在 Handler 正常返回后提交 offset。
func (p *KafkaProvider) Subscribe(ctx context.Context, subject string, handler func(*Message)) (func(), error) {
	if handler == nil {
		return nil, fmt.Errorf("kafka: handler is required")
	}
	group := mqResourceName(p.cfg.Prefix, "default."+subject, 255)
	return p.subscribe(ctx, subject, ReliableSubscribeOptions{Group: group}, func(message *Message) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logx.Errorw("mq_kafka_handler_panic",
					logx.Field("subject", subject),
					logx.Field("error", fmt.Sprint(recovered)),
				)
				err = fmt.Errorf("kafka: handler panic")
			}
		}()
		handler(message)
		return nil
	})
}

// SubscribeReliable 在 Handler 成功后同步提交 offset，失败时重试同一消息。
func (p *KafkaProvider) SubscribeReliable(
	ctx context.Context,
	subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) (func(), error) {
	if options.Group == "" || handler == nil {
		return nil, fmt.Errorf("kafka: reliable group and handler are required")
	}
	if options.KeyConcurrency > 1 {
		return nil, ErrKeyedReliableSubscribeUnsupported
	}
	return p.subscribe(ctx, subject, options, func(message *Message) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logx.Errorw("mq_kafka_handler_panic",
					logx.Field("subject", subject),
					logx.Field("error", fmt.Sprint(recovered)),
				)
				err = fmt.Errorf("kafka: handler panic")
			}
		}()
		return handler(message)
	})
}

func (p *KafkaProvider) subscribe(
	ctx context.Context,
	subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) (func(), error) {
	p.stateMu.Lock()
	if !p.connected || p.closed || p.readerFactory == nil {
		p.stateMu.Unlock()
		return nil, ErrNotConnected
	}
	consumerCtx, cancel := context.WithCancel(ctx)
	p.nextSubID++
	id := p.nextSubID
	subscription := &kafkaSubscription{cancel: cancel, done: make(chan struct{})}
	p.subscriptions[id] = subscription
	p.wg.Add(1)
	p.stateMu.Unlock()

	ready := make(chan error, 1)
	go p.runKafkaSubscription(consumerCtx, id, subject, options, subscription, handler, ready)
	abort := func() {
		cancel()
		subscription.closeReader()
	}
	if err := waitSubscriptionReady(consumerCtx, ready, subscription.done, abort); err != nil {
		return nil, err
	}
	var cancelOnce sync.Once
	return func() {
		cancelOnce.Do(func() {
			abort()
			<-subscription.done
		})
	}, nil
}

func (p *KafkaProvider) runKafkaSubscription(
	ctx context.Context,
	id uint64,
	subject string,
	options ReliableSubscribeOptions,
	subscription *kafkaSubscription,
	handler func(*Message) error,
	ready chan<- error,
) {
	defer p.wg.Done()
	defer close(subscription.done)
	defer func() {
		subscription.closeReader()
		p.stateMu.Lock()
		delete(p.subscriptions, id)
		p.stateMu.Unlock()
	}()

	topic := mqResourceName(p.cfg.Prefix, subject, 249)
	groupID := mqResourceName("", options.Group, 255)
	startOffset := kafkaStartOffset(p.cfg.StartOffset)
	backoff := 100 * time.Millisecond
	for {
		if ctx.Err() != nil {
			signalSubscriptionReady(ready, ctx.Err())
			return
		}
		p.stateMu.RLock()
		if p.closed || !p.connected || p.readerFactory == nil {
			p.stateMu.RUnlock()
			signalSubscriptionReady(ready, ErrNotConnected)
			return
		}
		factory := p.readerFactory
		dialer := cloneKafkaDialer(p.dialer, p.cfg.ClientID, options.Consumer)
		brokers := nonEmptyKafkaBrokers(p.cfg.Brokers)
		p.stateMu.RUnlock()

		if err := p.ensureTopics(ctx, topic); err != nil {
			if !waitMQRetry(ctx, backoff) {
				signalSubscriptionReady(ready, err)
				return
			}
			backoff = nextMQBackoff(backoff)
			continue
		}
		reader := factory(kafka.ReaderConfig{
			Brokers:        brokers,
			GroupID:        groupID,
			Topic:          topic,
			Dialer:         dialer,
			CommitInterval: 0,
			StartOffset:    startOffset,
		})
		if reader == nil {
			if !waitMQRetry(ctx, backoff) {
				signalSubscriptionReady(ready, ErrNotConnected)
				return
			}
			backoff = nextMQBackoff(backoff)
			continue
		}
		subscription.setReader(reader)
		signalSubscriptionReady(ready, nil)
		backoff = 100 * time.Millisecond

		err := p.consumeKafkaSession(ctx, subject, reader, handler)
		subscription.closeReader()
		if ctx.Err() != nil || err == nil {
			return
		}
		logx.Debugw("mq_kafka_subscription_reconnect",
			logx.Field("subject", subject),
			logx.Field("error", err),
		)
		if !waitMQRetry(ctx, backoff) {
			return
		}
		backoff = nextMQBackoff(backoff)
	}
}

func (p *KafkaProvider) consumeKafkaSession(
	ctx context.Context,
	subject string,
	reader kafkaMessageReader,
	handler func(*Message) error,
) error {
	for {
		brokerMessage, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		message := &Message{
			ID:      fmt.Sprintf("%s:%d:%d", brokerMessage.Topic, brokerMessage.Partition, brokerMessage.Offset),
			Subject: subject,
			Data:    brokerMessage.Value,
		}
		for {
			if err := handler(message); err != nil {
				if !waitMQRetry(ctx, 100*time.Millisecond) {
					return nil
				}
				continue
			}
			if err := reader.CommitMessages(ctx, brokerMessage); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			break
		}
	}
}

// Health 通过有界 metadata 请求检查 Kafka Broker。
func (p *KafkaProvider) Health(ctx context.Context) error {
	p.stateMu.RLock()
	if !p.connected || p.closed || p.dialer == nil {
		p.stateMu.RUnlock()
		return ErrNotConnected
	}
	dialer := p.dialer
	broker := firstKafkaBroker(p.cfg.Brokers)
	timeout := p.cfg.ConnectTimeout
	p.stateMu.RUnlock()

	checkCtx, cancel := boundedContext(ctx, timeout)
	defer cancel()
	connection, err := dialer.DialContext(checkCtx, "tcp", broker)
	if err != nil {
		return fmt.Errorf("kafka: health metadata connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.Brokers(); err != nil {
		return fmt.Errorf("kafka: health metadata request: %w", err)
	}
	return nil
}

// Close 停止全部 reader，并关闭 writer 与共享 transport。
func (p *KafkaProvider) Close() error {
	p.closeOnce.Do(func() {
		p.stateMu.Lock()
		p.closed = true
		p.connected = false
		writer := p.writer
		transport := p.transport
		p.writer = nil
		p.transport = nil
		subscriptions := make([]*kafkaSubscription, 0, len(p.subscriptions))
		for _, subscription := range p.subscriptions {
			subscriptions = append(subscriptions, subscription)
		}
		p.stateMu.Unlock()

		for _, subscription := range subscriptions {
			subscription.cancel()
			subscription.closeReader()
		}
		p.wg.Wait()
		if writer != nil {
			p.closeErr = errors.Join(p.closeErr, writer.Close())
		}
		if transport != nil {
			transport.CloseIdleConnections()
		}
	})
	return p.closeErr
}

func validateKafkaProviderConfig(cfg config.KafkaMQConfig) error {
	if len(nonEmptyKafkaBrokers(cfg.Brokers)) == 0 {
		return fmt.Errorf("brokers requires at least one non-empty address")
	}
	mechanism := strings.ToLower(strings.TrimSpace(cfg.SASL.Mechanism))
	switch mechanism {
	case "":
		if cfg.SASL.Username != "" || cfg.SASL.Password != "" {
			return fmt.Errorf("SASL mechanism is required when credentials are configured")
		}
	case "plain", "scram-sha-256", "scram-sha-512":
		if cfg.SASL.Username == "" {
			return fmt.Errorf("SASL username is required")
		}
		if cfg.SASL.Password == "" {
			return fmt.Errorf("SASL password is required")
		}
	default:
		return fmt.Errorf("SASL mechanism is unsupported")
	}
	if (cfg.TLS.CertFile == "") != (cfg.TLS.KeyFile == "") {
		return fmt.Errorf("TLS CertFile and KeyFile must be configured together")
	}
	return nil
}

func nonEmptyKafkaBrokers(brokers []string) []string {
	result := make([]string, 0, len(brokers))
	for _, broker := range brokers {
		if trimmed := strings.TrimSpace(broker); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func firstKafkaBroker(brokers []string) string {
	nonEmpty := nonEmptyKafkaBrokers(brokers)
	if len(nonEmpty) == 0 {
		return ""
	}
	return nonEmpty[0]
}

func kafkaSASLMechanism(cfg config.KafkaSASLConfig) (sasl.Mechanism, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Mechanism)) {
	case "":
		return nil, nil
	case "plain":
		return plain.Mechanism{Username: cfg.Username, Password: cfg.Password}, nil
	case "scram-sha-256":
		return scram.Mechanism(scram.SHA256, cfg.Username, cfg.Password)
	case "scram-sha-512":
		return scram.Mechanism(scram.SHA512, cfg.Username, cfg.Password)
	default:
		return nil, fmt.Errorf("unsupported mechanism")
	}
}

func (p *KafkaProvider) ensureTopics(ctx context.Context, topics ...string) error {
	p.stateMu.RLock()
	admin := p.topicAdmin
	p.stateMu.RUnlock()
	if admin == nil {
		return nil
	}
	return admin.EnsureTopics(ctx, topics...)
}

type kafkaTopicAdmin interface {
	EnsureTopics(ctx context.Context, topics ...string) error
}

type kafkaGoTopicAdmin struct {
	mu      sync.Mutex
	created map[string]struct{}
	client  *kafka.Client
}

func (a *kafkaGoTopicAdmin) EnsureTopics(ctx context.Context, topics ...string) error {
	if a == nil || a.client == nil {
		return nil
	}
	a.mu.Lock()
	if a.created == nil {
		a.created = make(map[string]struct{})
	}
	missing := make([]kafka.TopicConfig, 0, len(topics))
	for _, topic := range topics {
		if topic == "" {
			continue
		}
		if _, exists := a.created[topic]; exists {
			continue
		}
		missing = append(missing, kafka.TopicConfig{
			Topic:             topic,
			NumPartitions:     1,
			ReplicationFactor: 1,
		})
	}
	a.mu.Unlock()
	if len(missing) == 0 {
		return nil
	}
	resp, err := a.client.CreateTopics(ctx, &kafka.CreateTopicsRequest{
		Addr:   a.client.Addr,
		Topics: missing,
	})
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, cfg := range missing {
		if topicErr := resp.Errors[cfg.Topic]; topicErr != nil && !errors.Is(topicErr, kafka.TopicAlreadyExists) {
			return fmt.Errorf("create topic %s: %w", cfg.Topic, topicErr)
		}
		a.created[cfg.Topic] = struct{}{}
	}
	return nil
}

func signalSubscriptionReady(ready chan<- error, err error) {
	if ready == nil {
		return
	}
	select {
	case ready <- err:
	default:
	}
}

func waitSubscriptionReady(ctx context.Context, ready <-chan error, done <-chan struct{}, abort func()) error {
	select {
	case err := <-ready:
		if err != nil {
			abort()
			<-done
			return err
		}
		return nil
	case <-done:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrNotConnected
	case <-ctx.Done():
		abort()
		<-done
		return ctx.Err()
	}
}

func kafkaStartOffset(value string) int64 {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "earliest":
		return kafka.FirstOffset
	default:
		return kafka.LastOffset
	}
}

func cloneKafkaDialer(base *kafka.Dialer, clientID, consumer string) *kafka.Dialer {
	if base == nil {
		base = &kafka.Dialer{}
	}
	clone := *base
	clone.ClientID = clientID
	if consumer != "" {
		clone.ClientID = mqResourceName(clientID, consumer, 255)
	}
	return &clone
}

func boundedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, hasDeadline := parent.Deadline(); hasDeadline || timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}
