package mq

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRabbitConfirmation struct {
	acknowledged bool
	err          error
}

func (f fakeRabbitConfirmation) WaitContext(context.Context) (bool, error) {
	return f.acknowledged, f.err
}

type fakeRabbitPublisher struct {
	mu         sync.Mutex
	exchange   string
	routingKey string
	publishing amqp.Publishing
	confirm    rabbitPublishConfirmation
	publishErr error
	closed     bool
}

func (f *fakeRabbitPublisher) Publish(
	_ context.Context,
	exchange string,
	routingKey string,
	publishing amqp.Publishing,
) (rabbitPublishConfirmation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exchange = exchange
	f.routingKey = routingKey
	f.publishing = publishing
	return f.confirm, f.publishErr
}

func (*fakeRabbitPublisher) Health(context.Context, string) error { return nil }
func (f *fakeRabbitPublisher) IsClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}
func (f *fakeRabbitPublisher) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

type fakeRabbitAcknowledger struct {
	mu      sync.Mutex
	acks    int
	nacks   int
	requeue bool
	acked   chan struct{}
}

func (f *fakeRabbitAcknowledger) Ack(uint64, bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acks++
	if f.acked != nil {
		select {
		case f.acked <- struct{}{}:
		default:
		}
	}
	return nil
}

func (f *fakeRabbitAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nacks++
	f.requeue = requeue
	return nil
}

func (*fakeRabbitAcknowledger) Reject(uint64, bool) error { return nil }

type fakeRabbitConsumerSession struct {
	deliveries chan amqp.Delivery
	closeOnce  sync.Once
}

func (f *fakeRabbitConsumerSession) Deliveries() <-chan amqp.Delivery { return f.deliveries }
func (f *fakeRabbitConsumerSession) Close() error {
	f.closeOnce.Do(func() { close(f.deliveries) })
	return nil
}

type fakeRabbitConnection struct {
	mu         sync.Mutex
	publisher  rabbitPublisher
	session    rabbitConsumerSession
	consumerUp chan struct{}
	queueName  string
	closed     bool
}

func (f *fakeRabbitConnection) NewPublisher(string) (rabbitPublisher, error) {
	return f.publisher, nil
}

func (f *fakeRabbitConnection) NewConsumer(
	_ context.Context,
	_ string,
	queueName string,
	_ string,
	_ string,
	_ int,
) (rabbitConsumerSession, error) {
	f.mu.Lock()
	f.queueName = queueName
	f.mu.Unlock()
	if f.consumerUp != nil {
		select {
		case f.consumerUp <- struct{}{}:
		default:
		}
	}
	return f.session, nil
}

// TestRabbitMQProviderDefaultQueueUsesPrefixOnce 验证普通订阅队列遵循 prefix.group.subject。
func TestRabbitMQProviderDefaultQueueUsesPrefixOnce(t *testing.T) {
	session := &fakeRabbitConsumerSession{deliveries: make(chan amqp.Delivery)}
	consumerUp := make(chan struct{}, 1)
	publisher := &fakeRabbitPublisher{confirm: fakeRabbitConfirmation{acknowledged: true}}
	connection := &fakeRabbitConnection{publisher: publisher, session: session, consumerUp: consumerUp}
	provider := NewRabbitMQProvider(config.RabbitMQConfig{Exchange: "events", QueuePrefix: "core", Prefetch: 1})
	provider.stateMu.Lock()
	provider.connected = true
	provider.connection = connection
	provider.publisher = publisher
	provider.stateMu.Unlock()

	cancel, err := provider.Subscribe(context.Background(), "order:changed", func(*Message) {})
	require.NoError(t, err)
	select {
	case <-consumerUp:
	case <-time.After(time.Second):
		t.Fatal("RabbitMQ consumer 未启动")
	}
	connection.mu.Lock()
	assert.Equal(t, "core.default.order_changed", connection.queueName)
	connection.mu.Unlock()
	cancel()
	assert.False(t, connection.IsClosed(), "取消单个订阅不得关闭共享 RabbitMQ connection")
}

func (f *fakeRabbitConnection) IsClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (f *fakeRabbitConnection) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// TestRabbitMQProviderDoesNotDeclareOrderedReliable 验证首版不声明严格同键有序能力。
func TestRabbitMQProviderDoesNotDeclareOrderedReliable(t *testing.T) {
	var provider MQProvider = NewRabbitMQProvider(config.RabbitMQConfig{
		URL: "amqp://guest:guest@127.0.0.1:5672/", Exchange: "events", Prefetch: 1,
	})
	_, reliable := provider.(ReliableMQProvider)
	_, ordered := provider.(OrderedReliableMQProvider)
	assert.True(t, reliable)
	assert.False(t, ordered)
}

// TestRabbitMQProviderPublishWaitsForConfirmationAndMapsMetadata 验证持久消息与 confirm ACK。
func TestRabbitMQProviderPublishWaitsForConfirmationAndMapsMetadata(t *testing.T) {
	publisher := &fakeRabbitPublisher{confirm: fakeRabbitConfirmation{acknowledged: true}}
	provider := NewRabbitMQProvider(config.RabbitMQConfig{Exchange: "core.events", QueuePrefix: "core", Prefetch: 1})
	provider.stateMu.Lock()
	provider.connected = true
	provider.publisher = publisher
	provider.stateMu.Unlock()

	err := provider.Publish(context.Background(), "order:changed", []byte("payload"), &PublishOptions{
		OrderingKey:    "order-42",
		IdempotencyKey: "event-7",
	})
	require.NoError(t, err)
	assert.Equal(t, "core.events", publisher.exchange)
	assert.Equal(t, "order_changed", publisher.routingKey)
	assert.Equal(t, uint8(amqp.Persistent), publisher.publishing.DeliveryMode)
	assert.Equal(t, "event-7", publisher.publishing.MessageId)
	assert.Equal(t, "order-42", publisher.publishing.Headers["x-core-ordering-key"])
	assert.Equal(t, []byte("payload"), publisher.publishing.Body)
}

// TestRabbitMQProviderPublishRejectsNack 验证 Broker Nack 不会被当成发布成功。
func TestRabbitMQProviderPublishRejectsNack(t *testing.T) {
	publisher := &fakeRabbitPublisher{confirm: fakeRabbitConfirmation{acknowledged: false}}
	provider := NewRabbitMQProvider(config.RabbitMQConfig{Exchange: "core.events", Prefetch: 1})
	provider.stateMu.Lock()
	provider.connected = true
	provider.publisher = publisher
	provider.stateMu.Unlock()

	err := provider.Publish(context.Background(), "orders", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nack")
}

// TestRabbitMQProviderDeliveryACKsOnlyAfterHandlerSuccess 验证 Handler 成功后才 ACK。
func TestRabbitMQProviderDeliveryACKsOnlyAfterHandlerSuccess(t *testing.T) {
	acknowledger := &fakeRabbitAcknowledger{}
	delivery := amqp.Delivery{Acknowledger: acknowledger, DeliveryTag: 7, Body: []byte("payload")}
	provider := NewRabbitMQProvider(config.RabbitMQConfig{})

	err := provider.processRabbitDelivery("orders", delivery, func(message *Message) error {
		assert.Equal(t, "orders", message.Subject)
		assert.Nil(t, message.Ack, "RabbitMQ manual ACK 由 Provider 在 Handler 返回后统一执行")
		return nil
	})
	require.NoError(t, err)
	acknowledger.mu.Lock()
	defer acknowledger.mu.Unlock()
	assert.Equal(t, 1, acknowledger.acks)
	assert.Zero(t, acknowledger.nacks)
}

// TestRabbitMQProviderDeliveryNACKsAndRequeuesHandlerFailure 验证 Handler 失败不 ACK 并要求 Broker 重投。
func TestRabbitMQProviderDeliveryNACKsAndRequeuesHandlerFailure(t *testing.T) {
	acknowledger := &fakeRabbitAcknowledger{}
	delivery := amqp.Delivery{Acknowledger: acknowledger, DeliveryTag: 8, Body: []byte("payload")}
	provider := NewRabbitMQProvider(config.RabbitMQConfig{})

	err := provider.processRabbitDelivery("orders", delivery, func(*Message) error {
		return errors.New("retry")
	})
	require.NoError(t, err)
	acknowledger.mu.Lock()
	defer acknowledger.mu.Unlock()
	assert.Zero(t, acknowledger.acks)
	assert.Equal(t, 1, acknowledger.nacks)
	assert.True(t, acknowledger.requeue)
}

// TestRabbitMQProviderReliableReconnectsAfterConsumerChannelCloses 验证 supervisor 重建连接、拓扑和 consumer。
func TestRabbitMQProviderReliableReconnectsAfterConsumerChannelCloses(t *testing.T) {
	firstSession := &fakeRabbitConsumerSession{deliveries: make(chan amqp.Delivery)}
	secondSession := &fakeRabbitConsumerSession{deliveries: make(chan amqp.Delivery, 1)}
	firstUp := make(chan struct{}, 1)
	secondUp := make(chan struct{}, 1)
	firstPublisher := &fakeRabbitPublisher{confirm: fakeRabbitConfirmation{acknowledged: true}}
	secondPublisher := &fakeRabbitPublisher{confirm: fakeRabbitConfirmation{acknowledged: true}}
	firstConnection := &fakeRabbitConnection{publisher: firstPublisher, session: firstSession, consumerUp: firstUp}
	secondConnection := &fakeRabbitConnection{publisher: secondPublisher, session: secondSession, consumerUp: secondUp}

	provider := NewRabbitMQProvider(config.RabbitMQConfig{
		URL: "amqp://guest:guest@127.0.0.1:5672/", Exchange: "events", QueuePrefix: "core", Prefetch: 1,
	})
	provider.stateMu.Lock()
	provider.connected = true
	provider.connection = firstConnection
	provider.publisher = firstPublisher
	provider.dial = func(context.Context, string, amqp.Config) (rabbitConnection, error) {
		return secondConnection, nil
	}
	provider.stateMu.Unlock()

	acknowledger := &fakeRabbitAcknowledger{acked: make(chan struct{}, 1)}
	cancel, err := provider.SubscribeReliable(context.Background(), "orders", ReliableSubscribeOptions{
		Group: "order-service", Consumer: "order-1",
	}, func(*Message) error { return nil })
	require.NoError(t, err)
	defer cancel()

	select {
	case <-firstUp:
	case <-time.After(time.Second):
		t.Fatal("首个 RabbitMQ consumer 未启动")
	}
	require.NoError(t, firstSession.Close())
	select {
	case <-secondUp:
	case <-time.After(2 * time.Second):
		t.Fatal("RabbitMQ consumer 断线后未重连")
	}
	secondSession.deliveries <- amqp.Delivery{Acknowledger: acknowledger, DeliveryTag: 9, Body: []byte("payload")}
	select {
	case <-acknowledger.acked:
	case <-time.After(time.Second):
		t.Fatal("重连后的 RabbitMQ delivery 未 ACK")
	}
}

// TestRabbitMQProviderReliablePanicNACKsAndContinues 验证 Handler panic 不杀死 supervisor。
func TestRabbitMQProviderReliablePanicNACKsAndContinues(t *testing.T) {
	session := &fakeRabbitConsumerSession{deliveries: make(chan amqp.Delivery, 2)}
	consumerUp := make(chan struct{}, 1)
	publisher := &fakeRabbitPublisher{confirm: fakeRabbitConfirmation{acknowledged: true}}
	connection := &fakeRabbitConnection{publisher: publisher, session: session, consumerUp: consumerUp}
	provider := NewRabbitMQProvider(config.RabbitMQConfig{Exchange: "events", QueuePrefix: "core", Prefetch: 1})
	provider.stateMu.Lock()
	provider.connected = true
	provider.connection = connection
	provider.publisher = publisher
	provider.stateMu.Unlock()

	firstAck := &fakeRabbitAcknowledger{}
	secondAck := &fakeRabbitAcknowledger{acked: make(chan struct{}, 1)}
	attempts := 0
	cancel, err := provider.SubscribeReliable(context.Background(), "orders", ReliableSubscribeOptions{Group: "order-service"}, func(*Message) error {
		attempts++
		if attempts == 1 {
			panic("handler failed")
		}
		return nil
	})
	require.NoError(t, err)
	defer cancel()
	<-consumerUp
	session.deliveries <- amqp.Delivery{Acknowledger: firstAck, DeliveryTag: 10}
	session.deliveries <- amqp.Delivery{Acknowledger: secondAck, DeliveryTag: 10, Redelivered: true}

	select {
	case <-secondAck.acked:
	case <-time.After(time.Second):
		t.Fatal("panic 后 supervisor 未继续处理重投消息")
	}
	firstAck.mu.Lock()
	defer firstAck.mu.Unlock()
	assert.Equal(t, 1, firstAck.nacks)
	assert.True(t, firstAck.requeue)
}
