package mq

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/zeromicro/go-zero/core/logx"
)

var _ MQProvider = (*RabbitMQProvider)(nil)
var _ ReliableMQProvider = (*RabbitMQProvider)(nil)

type rabbitPublishConfirmation interface {
	WaitContext(ctx context.Context) (bool, error)
}

type rabbitPublisher interface {
	Publish(ctx context.Context, exchange, routingKey string, publishing amqp.Publishing) (rabbitPublishConfirmation, error)
	Health(ctx context.Context, exchange string) error
	IsClosed() bool
	Close() error
}

type rabbitConsumerSession interface {
	Deliveries() <-chan amqp.Delivery
	Close() error
}

type rabbitConnection interface {
	NewPublisher(exchange string) (rabbitPublisher, error)
	NewConsumer(
		ctx context.Context,
		exchange, queue, routingKey, consumer string,
		prefetch int,
	) (rabbitConsumerSession, error)
	IsClosed() bool
	Close() error
}

type rabbitDialer func(ctx context.Context, rawURL string, cfg amqp.Config) (rabbitConnection, error)

type rabbitSubscription struct {
	cancel context.CancelFunc
	done   chan struct{}

	sessionMu sync.Mutex
	session   rabbitConsumerSession
}

func (s *rabbitSubscription) setSession(session rabbitConsumerSession) {
	s.sessionMu.Lock()
	old := s.session
	s.session = session
	s.sessionMu.Unlock()
	if old != nil && old != session {
		_ = old.Close()
	}
}

func (s *rabbitSubscription) closeSession() {
	s.sessionMu.Lock()
	session := s.session
	s.session = nil
	s.sessionMu.Unlock()
	if session != nil {
		_ = session.Close()
	}
}

// RabbitMQProvider 使用 durable topic exchange、publisher confirm 与 manual ACK。
type RabbitMQProvider struct {
	cfg config.RabbitMQConfig

	stateMu       sync.RWMutex
	connected     bool
	closed        bool
	connection    rabbitConnection
	publisher     rabbitPublisher
	dial          rabbitDialer
	subscriptions map[uint64]*rabbitSubscription
	nextSubID     uint64

	reconnectMu sync.Mutex
	publishMu   sync.Mutex
	closeOnce   sync.Once
	closeErr    error
	wg          sync.WaitGroup
}

// NewRabbitMQProvider 创建尚未连接的 RabbitMQ Provider。
func NewRabbitMQProvider(cfg config.RabbitMQConfig) *RabbitMQProvider {
	if cfg.Exchange == "" {
		cfg.Exchange = "digitalway.core.events"
	}
	if cfg.QueuePrefix == "" {
		cfg.QueuePrefix = "digitalway-core"
	}
	if cfg.Prefetch == 0 {
		cfg.Prefetch = 1
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 10 * time.Second
	}
	return &RabbitMQProvider{
		cfg:           cfg,
		dial:          dialRabbitMQ,
		subscriptions: make(map[uint64]*rabbitSubscription),
	}
}

// Name 返回 RabbitMQ Provider 的稳定标识。
func (*RabbitMQProvider) Name() string { return "rabbitmq" }

// Connect 建立 AMQP 连接、声明 durable topic exchange 并启用 publisher confirm。
func (p *RabbitMQProvider) Connect(ctx context.Context) error {
	if err := validateRabbitMQProviderConfig(p.cfg); err != nil {
		return fmt.Errorf("%w: %v", ErrProviderConfiguration, err)
	}
	return p.ensureRabbitConnection(ctx)
}

func (p *RabbitMQProvider) ensureRabbitConnection(ctx context.Context) error {
	p.reconnectMu.Lock()
	defer p.reconnectMu.Unlock()

	p.stateMu.RLock()
	if p.closed {
		p.stateMu.RUnlock()
		return ErrNotConnected
	}
	if p.connected && p.connection != nil && !p.connection.IsClosed() && p.publisher != nil && !p.publisher.IsClosed() {
		p.stateMu.RUnlock()
		return nil
	}
	p.stateMu.RUnlock()

	tlsConfig, err := buildMQTLSConfig(p.cfg.TLS)
	if err != nil {
		return fmt.Errorf("%w: rabbitmq TLS: %v", ErrProviderConfiguration, err)
	}
	dialConfig := amqp.Config{
		TLSClientConfig: tlsConfig,
		Locale:          "en_US",
		Dial:            amqp.DefaultDial(p.cfg.ConnectTimeout),
	}
	connection, err := p.dial(ctx, p.cfg.URL, dialConfig)
	if err != nil {
		return fmt.Errorf("rabbitmq: connect %s: %w", redactedAMQPURL(p.cfg.URL), err)
	}
	publisher, err := connection.NewPublisher(p.cfg.Exchange)
	if err != nil {
		_ = connection.Close()
		return fmt.Errorf("rabbitmq: initialize publisher: %w", err)
	}

	p.stateMu.Lock()
	if p.closed {
		p.stateMu.Unlock()
		_ = publisher.Close()
		_ = connection.Close()
		return ErrNotConnected
	}
	oldPublisher := p.publisher
	oldConnection := p.connection
	p.publisher = publisher
	p.connection = connection
	p.connected = true
	p.stateMu.Unlock()
	if oldPublisher != nil && oldPublisher != publisher {
		_ = oldPublisher.Close()
	}
	if oldConnection != nil && oldConnection != connection {
		_ = oldConnection.Close()
	}
	return nil
}

// Publish 发布 persistent 消息并等待 Broker publisher confirmation。
func (p *RabbitMQProvider) Publish(ctx context.Context, subject string, data []byte, opts *PublishOptions) error {
	p.stateMu.RLock()
	if !p.connected || p.closed || p.publisher == nil || p.publisher.IsClosed() {
		p.stateMu.RUnlock()
		return ErrNotConnected
	}
	publisher := p.publisher
	exchange := p.cfg.Exchange
	p.stateMu.RUnlock()

	publishing := amqp.Publishing{
		ContentType:  "application/octet-stream",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Body:         data,
	}
	if opts != nil {
		publishing.MessageId = opts.IdempotencyKey
		if opts.OrderingKey != "" {
			publishing.Headers = amqp.Table{"x-core-ordering-key": opts.OrderingKey}
		}
	}
	routingKey := mqResourceName("", subject, 255)
	p.publishMu.Lock()
	defer p.publishMu.Unlock()
	confirmation, err := publisher.Publish(ctx, exchange, routingKey, publishing)
	if err != nil {
		return fmt.Errorf("rabbitmq: publish: %w", err)
	}
	if confirmation == nil {
		return fmt.Errorf("rabbitmq: publish confirmation unavailable")
	}
	acknowledged, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("rabbitmq: wait publisher confirmation: %w", err)
	}
	if !acknowledged {
		return fmt.Errorf("rabbitmq: publisher confirmation nack")
	}
	return nil
}

// Subscribe 使用默认 durable queue，并在 Handler 正常返回后 ACK。
func (p *RabbitMQProvider) Subscribe(ctx context.Context, subject string, handler func(*Message)) (func(), error) {
	if handler == nil {
		return nil, fmt.Errorf("rabbitmq: handler is required")
	}
	group := "default"
	return p.subscribe(ctx, subject, ReliableSubscribeOptions{Group: group}, func(message *Message) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logx.Errorw("mq_rabbitmq_handler_panic",
					logx.Field("subject", subject),
					logx.Field("error", fmt.Sprint(recovered)),
				)
				err = fmt.Errorf("rabbitmq: handler panic")
			}
		}()
		handler(message)
		return nil
	})
}

// SubscribeReliable 按逻辑服务组创建 durable queue，并以 manual ACK 消费。
func (p *RabbitMQProvider) SubscribeReliable(
	ctx context.Context,
	subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) (func(), error) {
	if options.Group == "" || handler == nil {
		return nil, fmt.Errorf("rabbitmq: reliable group and handler are required")
	}
	if options.KeyConcurrency > 1 {
		return nil, ErrKeyedReliableSubscribeUnsupported
	}
	return p.subscribe(ctx, subject, options, func(message *Message) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logx.Errorw("mq_rabbitmq_handler_panic",
					logx.Field("subject", subject),
					logx.Field("error", fmt.Sprint(recovered)),
				)
				err = fmt.Errorf("rabbitmq: handler panic")
			}
		}()
		return handler(message)
	})
}

func (p *RabbitMQProvider) subscribe(
	ctx context.Context,
	subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) (func(), error) {
	p.stateMu.Lock()
	if !p.connected || p.closed {
		p.stateMu.Unlock()
		return nil, ErrNotConnected
	}
	consumerCtx, cancel := context.WithCancel(ctx)
	p.nextSubID++
	id := p.nextSubID
	subscription := &rabbitSubscription{cancel: cancel, done: make(chan struct{})}
	p.subscriptions[id] = subscription
	p.wg.Add(1)
	p.stateMu.Unlock()

	go p.runRabbitSubscription(consumerCtx, id, subject, options, subscription, handler)
	var cancelOnce sync.Once
	return func() {
		cancelOnce.Do(func() {
			cancel()
			subscription.closeSession()
			<-subscription.done
		})
	}, nil
}

func (p *RabbitMQProvider) runRabbitSubscription(
	ctx context.Context,
	id uint64,
	subject string,
	options ReliableSubscribeOptions,
	subscription *rabbitSubscription,
	handler func(*Message) error,
) {
	defer p.wg.Done()
	defer close(subscription.done)
	defer func() {
		subscription.closeSession()
		p.stateMu.Lock()
		delete(p.subscriptions, id)
		p.stateMu.Unlock()
	}()

	backoff := 100 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return
		}
		if err := p.ensureRabbitConnection(ctx); err != nil {
			if !waitMQRetry(ctx, backoff) {
				return
			}
			backoff = nextMQBackoff(backoff)
			continue
		}
		p.stateMu.RLock()
		connection := p.connection
		exchange := p.cfg.Exchange
		queuePrefix := p.cfg.QueuePrefix
		prefetch := p.cfg.Prefetch
		p.stateMu.RUnlock()
		queueName := mqResourceName(queuePrefix, options.Group+"."+subject, 255)
		routingKey := mqResourceName("", subject, 255)
		consumerTag := mqResourceName(options.Group, options.Consumer, 255)
		session, err := connection.NewConsumer(ctx, exchange, queueName, routingKey, consumerTag, prefetch)
		if err != nil {
			p.invalidateRabbitConnection(connection)
			if !waitMQRetry(ctx, backoff) {
				return
			}
			backoff = nextMQBackoff(backoff)
			continue
		}
		subscription.setSession(session)
		backoff = 100 * time.Millisecond

		ended := false
		for !ended {
			select {
			case <-ctx.Done():
				return
			case delivery, ok := <-session.Deliveries():
				if !ok {
					ended = true
					continue
				}
				if err := p.processRabbitDelivery(ctx, subject, delivery, handler); err != nil {
					ended = true
				}
			}
		}
		subscription.closeSession()
		if ctx.Err() != nil {
			return
		}
		p.invalidateRabbitConnection(connection)
	}
}

func (p *RabbitMQProvider) processRabbitDelivery(
	ctx context.Context,
	subject string,
	delivery amqp.Delivery,
	handler func(*Message) error,
) error {
	messageID := delivery.MessageId
	if messageID == "" {
		messageID = fmt.Sprintf("%s:%d", delivery.RoutingKey, delivery.DeliveryTag)
	}
	message := &Message{
		ID:      messageID,
		Subject: subject,
		Data:    delivery.Body,
	}
	if err := handler(message); err != nil {
		if nackErr := delivery.Nack(false, true); nackErr != nil {
			return fmt.Errorf("rabbitmq: nack delivery: %w", nackErr)
		}
		if !waitMQRetry(ctx, 50*time.Millisecond) {
			return ctx.Err()
		}
		return nil
	}
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("rabbitmq: ack delivery: %w", err)
	}
	return nil
}

func (p *RabbitMQProvider) invalidateRabbitConnection(connection rabbitConnection) {
	p.reconnectMu.Lock()
	p.stateMu.Lock()
	if p.connection != connection {
		p.stateMu.Unlock()
		p.reconnectMu.Unlock()
		return
	}
	publisher := p.publisher
	p.connection = nil
	p.publisher = nil
	p.connected = false
	p.stateMu.Unlock()
	if publisher != nil {
		_ = publisher.Close()
	}
	if connection != nil {
		_ = connection.Close()
	}
	p.reconnectMu.Unlock()
}

// Health 通过连接状态与 passive exchange declare 检查 RabbitMQ。
func (p *RabbitMQProvider) Health(ctx context.Context) error {
	p.stateMu.RLock()
	if !p.connected || p.closed || p.connection == nil || p.connection.IsClosed() || p.publisher == nil || p.publisher.IsClosed() {
		p.stateMu.RUnlock()
		return ErrNotConnected
	}
	publisher := p.publisher
	exchange := p.cfg.Exchange
	p.stateMu.RUnlock()
	p.publishMu.Lock()
	defer p.publishMu.Unlock()
	if err := publisher.Health(ctx, exchange); err != nil {
		return fmt.Errorf("rabbitmq: health: %w", err)
	}
	return nil
}

// Close 禁止重连，取消全部 consumer，并关闭 channel 与 connection。
func (p *RabbitMQProvider) Close() error {
	p.closeOnce.Do(func() {
		p.stateMu.Lock()
		p.closed = true
		p.connected = false
		publisher := p.publisher
		connection := p.connection
		p.publisher = nil
		p.connection = nil
		subscriptions := make([]*rabbitSubscription, 0, len(p.subscriptions))
		for _, subscription := range p.subscriptions {
			subscriptions = append(subscriptions, subscription)
		}
		p.stateMu.Unlock()

		for _, subscription := range subscriptions {
			subscription.cancel()
			subscription.closeSession()
		}
		p.wg.Wait()
		if publisher != nil {
			p.closeErr = errors.Join(p.closeErr, publisher.Close())
		}
		if connection != nil {
			p.closeErr = errors.Join(p.closeErr, connection.Close())
		}
	})
	return p.closeErr
}

func validateRabbitMQProviderConfig(cfg config.RabbitMQConfig) error {
	parsed, err := url.Parse(cfg.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("URL is required and must be valid")
	}
	if parsed.Scheme != "amqp" && parsed.Scheme != "amqps" {
		return fmt.Errorf("URL scheme must be amqp or amqps")
	}
	if strings.TrimSpace(cfg.Exchange) == "" {
		return fmt.Errorf("exchange is required")
	}
	if cfg.Prefetch <= 0 {
		return fmt.Errorf("prefetch must be greater than zero")
	}
	if (cfg.TLS.CertFile == "") != (cfg.TLS.KeyFile == "") {
		return fmt.Errorf("TLS CertFile and KeyFile must be configured together")
	}
	return nil
}

func waitMQRetry(ctx context.Context, delay time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

func nextMQBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > 2*time.Second {
		return 2 * time.Second
	}
	return next
}

func dialRabbitMQ(_ context.Context, rawURL string, cfg amqp.Config) (rabbitConnection, error) {
	connection, err := amqp.DialConfig(rawURL, cfg)
	if err != nil {
		return nil, err
	}
	return &amqpRabbitConnection{connection: connection}, nil
}

type amqpRabbitConnection struct {
	connection *amqp.Connection
}

func (c *amqpRabbitConnection) NewPublisher(exchange string) (rabbitPublisher, error) {
	channel, err := c.connection.Channel()
	if err != nil {
		return nil, err
	}
	if err := channel.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		_ = channel.Close()
		return nil, err
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		return nil, err
	}
	return &amqpRabbitPublisher{channel: channel}, nil
}

func (c *amqpRabbitConnection) NewConsumer(
	ctx context.Context,
	exchange, queueName, routingKey, consumer string,
	prefetch int,
) (rabbitConsumerSession, error) {
	channel, err := c.connection.Channel()
	if err != nil {
		return nil, err
	}
	closeOnError := func(err error) (rabbitConsumerSession, error) {
		_ = channel.Close()
		return nil, err
	}
	if err := channel.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		return closeOnError(err)
	}
	if err := channel.Qos(prefetch, 0, false); err != nil {
		return closeOnError(err)
	}
	queue, err := channel.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		return closeOnError(err)
	}
	if err := channel.QueueBind(queue.Name, routingKey, exchange, false, nil); err != nil {
		return closeOnError(err)
	}
	deliveries, err := channel.ConsumeWithContext(ctx, queue.Name, consumer, false, false, false, false, nil)
	if err != nil {
		return closeOnError(err)
	}
	return &amqpRabbitConsumerSession{channel: channel, deliveries: deliveries}, nil
}

func (c *amqpRabbitConnection) IsClosed() bool { return c.connection.IsClosed() }
func (c *amqpRabbitConnection) Close() error   { return c.connection.Close() }

type amqpRabbitPublisher struct {
	channel *amqp.Channel
}

func (p *amqpRabbitPublisher) Publish(
	ctx context.Context,
	exchange, routingKey string,
	publishing amqp.Publishing,
) (rabbitPublishConfirmation, error) {
	return p.channel.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, false, false, publishing)
}

func (p *amqpRabbitPublisher) Health(_ context.Context, exchange string) error {
	return p.channel.ExchangeDeclarePassive(exchange, "topic", true, false, false, false, nil)
}

func (p *amqpRabbitPublisher) IsClosed() bool { return p.channel.IsClosed() }
func (p *amqpRabbitPublisher) Close() error   { return p.channel.Close() }

type amqpRabbitConsumerSession struct {
	channel    *amqp.Channel
	deliveries <-chan amqp.Delivery
}

func (s *amqpRabbitConsumerSession) Deliveries() <-chan amqp.Delivery { return s.deliveries }
func (s *amqpRabbitConsumerSession) Close() error                     { return s.channel.Close() }
