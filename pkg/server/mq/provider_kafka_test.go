package mq

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeKafkaWriter struct {
	mu       sync.Mutex
	messages []kafka.Message
	err      error
	closed   bool
}

func (f *fakeKafkaWriter) WriteMessages(_ context.Context, messages ...kafka.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, messages...)
	return f.err
}

func (f *fakeKafkaWriter) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

type fakeKafkaReader struct {
	message   kafka.Message
	fetched   bool
	committed chan kafka.Message
	closed    chan struct{}
	closeOnce sync.Once
}

func (f *fakeKafkaReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	if !f.fetched {
		f.fetched = true
		return f.message, nil
	}
	<-ctx.Done()
	return kafka.Message{}, ctx.Err()
}

func (f *fakeKafkaReader) CommitMessages(_ context.Context, messages ...kafka.Message) error {
	if len(messages) > 0 {
		f.committed <- messages[0]
	}
	return nil
}

func (f *fakeKafkaReader) Close() error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

// TestKafkaProviderDoesNotDeclareOrderedReliable 验证首版只声明可靠消费，不夸大有序能力。
func TestKafkaProviderDoesNotDeclareOrderedReliable(t *testing.T) {
	var provider MQProvider = NewKafkaProvider(config.KafkaMQConfig{Brokers: []string{"127.0.0.1:9092"}})
	_, reliable := provider.(ReliableMQProvider)
	_, ordered := provider.(OrderedReliableMQProvider)
	assert.True(t, reliable)
	assert.False(t, ordered)
}

// TestKafkaProviderPublishMapsTopicKeyAndIdempotencyHeader 验证同步发布的 Broker 元数据映射。
func TestKafkaProviderPublishMapsTopicKeyAndIdempotencyHeader(t *testing.T) {
	writer := &fakeKafkaWriter{}
	provider := NewKafkaProvider(config.KafkaMQConfig{Prefix: "core"})
	provider.stateMu.Lock()
	provider.connected = true
	provider.writer = writer
	provider.stateMu.Unlock()

	err := provider.Publish(context.Background(), "order:changed", []byte("payload"), &PublishOptions{
		OrderingKey:    "order-42",
		IdempotencyKey: "event-7",
	})
	require.NoError(t, err)
	require.Len(t, writer.messages, 1)
	message := writer.messages[0]
	assert.Equal(t, "core.order_changed", message.Topic)
	assert.Equal(t, []byte("order-42"), message.Key)
	assert.Equal(t, []byte("payload"), message.Value)
	require.Len(t, message.Headers, 1)
	assert.Equal(t, "core-idempotency-key", message.Headers[0].Key)
	assert.Equal(t, []byte("event-7"), message.Headers[0].Value)
}

// TestKafkaProviderReliableRetriesBeforeCommit 验证 Handler 失败不 commit，并在成功后才确认同一消息。
func TestKafkaProviderReliableRetriesBeforeCommit(t *testing.T) {
	reader := &fakeKafkaReader{
		message:   kafka.Message{Topic: "core.orders", Partition: 2, Offset: 42, Value: []byte("payload")},
		committed: make(chan kafka.Message, 1),
		closed:    make(chan struct{}),
	}
	provider := NewKafkaProvider(config.KafkaMQConfig{Prefix: "core", ClientID: "core-client"})
	provider.stateMu.Lock()
	provider.connected = true
	provider.readerFactory = func(readerConfig kafka.ReaderConfig) kafkaMessageReader {
		assert.Equal(t, "core.orders", readerConfig.Topic)
		assert.Equal(t, "order-service", readerConfig.GroupID)
		return reader
	}
	provider.stateMu.Unlock()

	var attempts int
	cancel, err := provider.SubscribeReliable(context.Background(), "orders", ReliableSubscribeOptions{
		Group:    "order-service",
		Consumer: "order-1",
	}, func(message *Message) error {
		attempts++
		assert.Equal(t, "core.orders:2:42", message.ID)
		assert.Equal(t, "orders", message.Subject)
		if attempts == 1 {
			return errors.New("retry")
		}
		return nil
	})
	require.NoError(t, err)

	select {
	case committed := <-reader.committed:
		assert.Equal(t, int64(42), committed.Offset)
	case <-time.After(2 * time.Second):
		t.Fatal("等待 Kafka commit 超时")
	}
	assert.Equal(t, 2, attempts)
	cancel()
	select {
	case <-reader.closed:
	case <-time.After(time.Second):
		t.Fatal("取消订阅后 reader 未关闭")
	}
}

// TestKafkaProviderConnectRejectsInvalidSASLBeforeNetwork 验证公共构造函数直用时也不会绕过配置校验。
func TestKafkaProviderConnectRejectsInvalidSASLBeforeNetwork(t *testing.T) {
	provider := NewKafkaProvider(config.KafkaMQConfig{
		Brokers: []string{"127.0.0.1:0"},
		SASL: config.KafkaSASLConfig{
			Mechanism: "plain",
			Username:  "alice",
		},
	})

	err := provider.Connect(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProviderConfiguration)
	assert.Contains(t, err.Error(), "password")
}

// TestKafkaProviderCloseIsIdempotentAndRejectsNewCalls 验证关闭只执行一次且永久拒绝新调用。
func TestKafkaProviderCloseIsIdempotentAndRejectsNewCalls(t *testing.T) {
	writer := &fakeKafkaWriter{}
	provider := NewKafkaProvider(config.KafkaMQConfig{Prefix: "core"})
	provider.stateMu.Lock()
	provider.connected = true
	provider.writer = writer
	provider.stateMu.Unlock()

	require.NoError(t, provider.Close())
	require.NoError(t, provider.Close())
	assert.ErrorIs(t, provider.Publish(context.Background(), "orders", nil, nil), ErrNotConnected)
	assert.ErrorIs(t, provider.Health(context.Background()), ErrNotConnected)
	assert.True(t, writer.closed)
}
