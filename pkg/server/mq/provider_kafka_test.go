package mq

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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

type fakeKafkaTopicAdmin struct {
	mu     sync.Mutex
	topics []string
}

func (f *fakeKafkaTopicAdmin) EnsureTopics(_ context.Context, topics ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.topics = append(f.topics, topics...)
	return nil
}

type fakeKafkaReader struct {
	mu        sync.Mutex
	messages  []kafka.Message
	index     int
	fetchErr  error
	commitErr error
	committed chan kafka.Message
	closed    chan struct{}
	closeOnce sync.Once
}

func (f *fakeKafkaReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	f.mu.Lock()
	if f.fetchErr != nil {
		err := f.fetchErr
		f.mu.Unlock()
		return kafka.Message{}, err
	}
	if f.index < len(f.messages) {
		message := f.messages[f.index]
		f.index++
		f.mu.Unlock()
		return message, nil
	}
	closed := f.closed
	f.mu.Unlock()
	select {
	case <-ctx.Done():
		return kafka.Message{}, ctx.Err()
	case <-closed:
		return kafka.Message{}, errors.New("reader closed")
	}
}

func (f *fakeKafkaReader) CommitMessages(_ context.Context, messages ...kafka.Message) error {
	f.mu.Lock()
	err := f.commitErr
	committed := f.committed
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if len(messages) > 0 && committed != nil {
		committed <- messages[0]
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
	_, keyed := provider.(KeyedReliableMQProvider)
	assert.True(t, reliable)
	assert.False(t, ordered)
	assert.False(t, keyed)
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

// TestKafkaProviderPublishCreatesMissingTopic 验证动态 subject 在首次发布前声明 topic。
func TestKafkaProviderPublishCreatesMissingTopic(t *testing.T) {
	writer := &fakeKafkaWriter{}
	admin := &fakeKafkaTopicAdmin{}
	provider := NewKafkaProvider(config.KafkaMQConfig{Prefix: "core"})
	provider.stateMu.Lock()
	provider.connected = true
	provider.writer = writer
	provider.topicAdmin = admin
	provider.stateMu.Unlock()

	require.NoError(t, provider.Publish(context.Background(), "order:changed", []byte("payload"), nil))
	admin.mu.Lock()
	defer admin.mu.Unlock()
	require.Equal(t, []string{"core.order_changed"}, admin.topics)
}

// TestKafkaProviderReliableRetriesBeforeCommit 验证 Handler 失败不 commit，并在成功后才确认同一消息。
func TestKafkaProviderReliableRetriesBeforeCommit(t *testing.T) {
	reader := &fakeKafkaReader{
		messages:  []kafka.Message{{Topic: "core.orders", Partition: 2, Offset: 42, Value: []byte("payload")}},
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

func connectedKafkaProvider(t *testing.T, factory func(kafka.ReaderConfig) kafkaMessageReader) *KafkaProvider {
	t.Helper()
	provider := NewKafkaProvider(config.KafkaMQConfig{Prefix: "core", ClientID: "core-client"})
	provider.stateMu.Lock()
	provider.connected = true
	provider.readerFactory = factory
	provider.stateMu.Unlock()
	return provider
}

// TestKafkaProviderSubscribeReturnsAfterReaderReady 验证 Subscribe 返回时 reader 已创建，避免发布落到尚未存在的消费组。
func TestKafkaProviderSubscribeReturnsAfterReaderReady(t *testing.T) {
	var created atomic.Bool
	provider := connectedKafkaProvider(t, func(kafka.ReaderConfig) kafkaMessageReader {
		created.Store(true)
		return &fakeKafkaReader{closed: make(chan struct{})}
	})

	cancel, err := provider.SubscribeReliable(context.Background(), "orders", ReliableSubscribeOptions{
		Group: "order-service",
	}, func(*Message) error { return nil })
	require.NoError(t, err)
	defer cancel()
	assert.True(t, created.Load())
}

// TestKafkaProviderSubscribeReliableRejectsKeyConcurrency 验证显式分键并发不得静默串行。
func TestKafkaProviderSubscribeReliableRejectsKeyConcurrency(t *testing.T) {
	provider := connectedKafkaProvider(t, func(kafka.ReaderConfig) kafkaMessageReader {
		t.Fatal("KeyConcurrency>1 不得创建 Kafka reader")
		return nil
	})
	_, err := provider.SubscribeReliable(context.Background(), "orders", ReliableSubscribeOptions{
		Group: "order-service", KeyConcurrency: 2,
	}, func(*Message) error { return nil })
	require.ErrorIs(t, err, ErrKeyedReliableSubscribeUnsupported)
}

// TestKafkaProviderNormalizesGroupIDAndStartsFromLatest 验证新消费组从当前末尾读，且 GroupID 走资源名规范化。
func TestKafkaProviderNormalizesGroupIDAndStartsFromLatest(t *testing.T) {
	got := make(chan kafka.ReaderConfig, 1)
	provider := connectedKafkaProvider(t, func(readerConfig kafka.ReaderConfig) kafkaMessageReader {
		got <- readerConfig
		return &fakeKafkaReader{closed: make(chan struct{})}
	})

	cancel, err := provider.SubscribeReliable(context.Background(), "order:changed", ReliableSubscribeOptions{
		Group: "order:service", Consumer: "order-1",
	}, func(*Message) error { return nil })
	require.NoError(t, err)
	defer cancel()

	select {
	case readerConfig := <-got:
		assert.Equal(t, "core.order_changed", readerConfig.Topic)
		assert.Equal(t, "order_service", readerConfig.GroupID)
		assert.Equal(t, kafka.LastOffset, readerConfig.StartOffset)
		assert.Zero(t, readerConfig.CommitInterval)
	case <-time.After(time.Second):
		t.Fatal("未捕获 Kafka reader 配置")
	}
}

// TestKafkaProviderEarliestStartOffsetIsConfigurable 验证显式 earliest 才从 topic 开头回放。
func TestKafkaProviderEarliestStartOffsetIsConfigurable(t *testing.T) {
	got := make(chan kafka.ReaderConfig, 1)
	provider := NewKafkaProvider(config.KafkaMQConfig{Prefix: "core", StartOffset: "earliest"})
	provider.stateMu.Lock()
	provider.connected = true
	provider.readerFactory = func(readerConfig kafka.ReaderConfig) kafkaMessageReader {
		got <- readerConfig
		return &fakeKafkaReader{closed: make(chan struct{})}
	}
	provider.stateMu.Unlock()

	cancel, err := provider.SubscribeReliable(context.Background(), "orders", ReliableSubscribeOptions{
		Group: "order-service",
	}, func(*Message) error { return nil })
	require.NoError(t, err)
	defer cancel()

	select {
	case readerConfig := <-got:
		assert.Equal(t, kafka.FirstOffset, readerConfig.StartOffset)
	case <-time.After(time.Second):
		t.Fatal("未捕获 Kafka earliest StartOffset")
	}
}

// TestKafkaProviderReliableRebuildsReaderAfterFetchFailure 验证 Fetch 失败后 supervisor 重建 reader，而不是让可靠订阅静默消失。
func TestKafkaProviderReliableRebuildsReaderAfterFetchFailure(t *testing.T) {
	first := &fakeKafkaReader{fetchErr: errors.New("broker unavailable"), closed: make(chan struct{})}
	second := &fakeKafkaReader{
		messages:  []kafka.Message{{Topic: "core.orders", Partition: 0, Offset: 7, Value: []byte("payload")}},
		committed: make(chan kafka.Message, 1),
		closed:    make(chan struct{}),
	}
	readers := []kafkaMessageReader{first, second}
	var calls atomic.Int32
	provider := connectedKafkaProvider(t, func(kafka.ReaderConfig) kafkaMessageReader {
		index := int(calls.Add(1) - 1)
		require.Less(t, index, len(readers))
		return readers[index]
	})

	cancel, err := provider.SubscribeReliable(context.Background(), "orders", ReliableSubscribeOptions{
		Group: "order-service",
	}, func(*Message) error { return nil })
	require.NoError(t, err)
	defer cancel()

	select {
	case committed := <-second.committed:
		assert.Equal(t, int64(7), committed.Offset)
	case <-time.After(2 * time.Second):
		t.Fatal("Fetch 失败后未重建 Kafka reader")
	}
}

// TestKafkaProviderReliableRebuildsReaderAfterCommitFailure 验证 commit 失败后重建 reader，未确认消息可被再次投递。
func TestKafkaProviderReliableRebuildsReaderAfterCommitFailure(t *testing.T) {
	first := &fakeKafkaReader{
		messages:  []kafka.Message{{Topic: "core.orders", Partition: 1, Offset: 3, Value: []byte("payload")}},
		commitErr: errors.New("coordinator unavailable"),
		closed:    make(chan struct{}),
	}
	second := &fakeKafkaReader{
		messages:  []kafka.Message{{Topic: "core.orders", Partition: 1, Offset: 3, Value: []byte("payload")}},
		committed: make(chan kafka.Message, 1),
		closed:    make(chan struct{}),
	}
	readers := []kafkaMessageReader{first, second}
	var calls atomic.Int32
	provider := connectedKafkaProvider(t, func(kafka.ReaderConfig) kafkaMessageReader {
		index := int(calls.Add(1) - 1)
		require.Less(t, index, len(readers))
		return readers[index]
	})

	var attempts int
	cancel, err := provider.SubscribeReliable(context.Background(), "orders", ReliableSubscribeOptions{
		Group: "order-service",
	}, func(*Message) error {
		attempts++
		return nil
	})
	require.NoError(t, err)
	defer cancel()

	select {
	case committed := <-second.committed:
		assert.Equal(t, int64(3), committed.Offset)
	case <-time.After(2 * time.Second):
		t.Fatal("commit 失败后未重建 Kafka reader")
	}
	assert.GreaterOrEqual(t, attempts, 2)
}

// TestKafkaProviderConcurrentCloseAndPublish 验证关闭与发布并发时不 panic，关闭后拒绝新发布。
func TestKafkaProviderConcurrentCloseAndPublish(t *testing.T) {
	writer := &fakeKafkaWriter{}
	provider := NewKafkaProvider(config.KafkaMQConfig{Prefix: "core"})
	provider.stateMu.Lock()
	provider.connected = true
	provider.writer = writer
	provider.stateMu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = provider.Publish(context.Background(), "orders", []byte("payload"), nil)
		}()
		go func() {
			defer wg.Done()
			_ = provider.Close()
		}()
	}
	wg.Wait()
	assert.ErrorIs(t, provider.Publish(context.Background(), "orders", nil, nil), ErrNotConnected)
	assert.True(t, writer.closed)
}
