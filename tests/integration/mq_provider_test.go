//go:build integration

// Package integration_test contains integration tests that require external services.
//
// MQ provider tests are gated by environment variables:
//
// CORE_TEST_REDIS_STREAM=1  – run Redis Streams contract tests.
//
//	Optional: CORE_TEST_REDIS_ADDR (default "127.0.0.1:6379")
//
// CORE_TEST_NATS=1          – run NATS JetStream contract tests.
//
//	Optional: CORE_TEST_NATS_URL (default "nats://127.0.0.1:4222")
//
// CORE_TEST_KAFKA=1         – run Kafka contract tests.
//
//	Optional: CORE_TEST_KAFKA_BROKERS (default "127.0.0.1:9092")
//
// CORE_TEST_RABBITMQ=1      – run RabbitMQ contract tests.
//
//	Optional: CORE_TEST_RABBITMQ_URL (default "amqp://core:core_test_password@127.0.0.1:5672/")
//
// To run all MQ integration tests:
//
// CORE_TEST_REDIS_STREAM=1 CORE_TEST_NATS=1 CORE_TEST_KAFKA=1 CORE_TEST_RABBITMQ=1 go test -tags=integration ./tests/integration/ -run TestMQ
package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runMQContract runs a standard publish→subscribe→ack→health contract against
// any MQProvider implementation.
func runMQContract(t *testing.T, p mq.MQProvider) {
	t.Helper()
	ctx := context.Background()

	require.NoError(t, p.Connect(ctx), "Connect should succeed")
	t.Cleanup(func() { _ = p.Close() })

	require.NoError(t, p.Health(ctx), "Health should return nil when connected")

	subject := fmt.Sprintf("core.integration.test.%d", time.Now().UnixNano())
	payload := []byte("hello-integration")

	var (
		mu       sync.Mutex
		received [][]byte
		ackErr   error
	)
	done := make(chan struct{})

	cancel, err := p.Subscribe(ctx, subject, func(msg *mq.Message) {
		mu.Lock()
		received = append(received, msg.Data)
		if msg.Ack != nil {
			ackErr = msg.Ack()
		}
		mu.Unlock()
		close(done)
	})
	require.NoError(t, err, "Subscribe should succeed")
	defer cancel()

	require.NoError(t, p.Publish(ctx, subject, payload, nil), "Publish should succeed")

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for message delivery")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, 1, "exactly one message should be delivered")
	assert.Equal(t, payload, received[0], "message payload should match")
	assert.NoError(t, ackErr, "Ack should not return an error")
}

// TestMQRedisStream runs the MQ contract against Redis Streams.
// Requires CORE_TEST_REDIS_STREAM=1.
func TestMQRedisStream(t *testing.T) {
	if os.Getenv("CORE_TEST_REDIS_STREAM") == "" {
		t.Skip("CORE_TEST_REDIS_STREAM not set – skipping Redis Streams integration test")
	}
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	p := mq.NewRedisStreamProvider(addr, "core-integration", 0)
	runMQContract(t, p)
}

// TestMQNATSJetStream runs the MQ contract against NATS JetStream.
// Requires CORE_TEST_NATS=1.
func TestMQNATSJetStream(t *testing.T) {
	if os.Getenv("CORE_TEST_NATS") == "" {
		t.Skip("CORE_TEST_NATS not set – skipping NATS JetStream integration test")
	}
	url := os.Getenv("CORE_TEST_NATS_URL")
	if url == "" {
		url = "nats://127.0.0.1:4222"
	}
	p := mq.NewNATSJetStreamProvider(url, "core-integration", "core-int")
	runMQContract(t, p)
}

// TestMQKafka 通过真实 Kafka 验证普通发布订阅与 Broker 健康。
func TestMQKafka(t *testing.T) {
	if os.Getenv("CORE_TEST_KAFKA") == "" {
		t.Skip("CORE_TEST_KAFKA not set")
	}
	p := mq.NewKafkaProvider(config.KafkaMQConfig{
		Brokers:     kafkaTestBrokers(),
		Prefix:      "core-integration",
		ClientID:    "core-integration",
		StartOffset: "earliest",
	})
	runMQContract(t, p)
}

// TestMQRabbitMQ 通过真实 RabbitMQ 验证 publisher confirm 与普通订阅。
func TestMQRabbitMQ(t *testing.T) {
	if os.Getenv("CORE_TEST_RABBITMQ") == "" {
		t.Skip("CORE_TEST_RABBITMQ not set")
	}
	p := mq.NewRabbitMQProvider(config.RabbitMQConfig{
		URL:         rabbitMQTestURL(),
		Exchange:    "core.integration.events",
		QueuePrefix: "core-integration",
		Prefetch:    1,
	})
	runMQContract(t, p)
}

// TestMQKafkaReliable 通过真实 Kafka 验证失败不确认、同消息重试和消费组隔离。
func TestMQKafkaReliable(t *testing.T) {
	if os.Getenv("CORE_TEST_KAFKA") == "" {
		t.Skip("CORE_TEST_KAFKA not set")
	}
	runReliableMQContract(t, mq.NewKafkaProvider(config.KafkaMQConfig{
		Brokers:     kafkaTestBrokers(),
		Prefix:      "core-reliable",
		ClientID:    "core-reliable",
		StartOffset: "earliest",
	}))
}

// TestMQRabbitMQReliable 通过真实 RabbitMQ 验证 NACK 重投、成功 ACK 和消费组隔离。
func TestMQRabbitMQReliable(t *testing.T) {
	if os.Getenv("CORE_TEST_RABBITMQ") == "" {
		t.Skip("CORE_TEST_RABBITMQ not set")
	}
	runReliableMQContract(t, mq.NewRabbitMQProvider(config.RabbitMQConfig{
		URL:         rabbitMQTestURL(),
		Exchange:    "core.reliable.events",
		QueuePrefix: "core-reliable",
		Prefetch:    1,
	}))
}

func runReliableMQContract(t *testing.T, provider mq.MQProvider) {
	t.Helper()
	ctx, cancelContext := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelContext()
	require.NoError(t, provider.Connect(ctx))
	manager := mq.NewManager()
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent(provider.Name()))
	t.Cleanup(func() { _ = manager.Close() })
	assert.ErrorIs(t, manager.RequireOrderedReliable(), mq.ErrOrderedReliableUnsupported)

	subject := fmt.Sprintf("core.reliable.%d", time.Now().UnixNano())
	payload := []byte("reliable-payload")
	groupADone := make(chan struct{}, 1)
	groupBDone := make(chan struct{}, 1)
	var mu sync.Mutex
	attemptsA := 0
	messageIDs := make([]string, 0, 2)
	cancelA, err := manager.SubscribeReliable(ctx, subject, mq.ReliableSubscribeOptions{
		Group: "group-a", Consumer: "consumer-a",
	}, func(message *mq.Message) error {
		mu.Lock()
		defer mu.Unlock()
		attemptsA++
		messageIDs = append(messageIDs, message.ID)
		if attemptsA == 1 {
			return fmt.Errorf("expected first-attempt failure")
		}
		select {
		case groupADone <- struct{}{}:
		default:
		}
		return nil
	})
	require.NoError(t, err)
	defer cancelA()
	cancelB, err := manager.SubscribeReliable(ctx, subject, mq.ReliableSubscribeOptions{
		Group: "group-b", Consumer: "consumer-b",
	}, func(message *mq.Message) error {
		assert.Equal(t, payload, message.Data)
		select {
		case groupBDone <- struct{}{}:
		default:
		}
		return nil
	})
	require.NoError(t, err)
	defer cancelB()

	require.NoError(t, manager.Publish(ctx, subject, payload, &mq.PublishOptions{
		OrderingKey: "order-42", IdempotencyKey: "event-42",
	}))
	for name, done := range map[string]<-chan struct{}{"group-a": groupADone, "group-b": groupBDone} {
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatalf("等待 %s 可靠消费超时: %v", name, ctx.Err())
		}
	}
	mu.Lock()
	require.Equal(t, 2, attemptsA)
	require.Len(t, messageIDs, 2)
	assert.Equal(t, messageIDs[0], messageIDs[1], "Handler 失败后必须重投同一消息")
	mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	assert.Equal(t, 2, attemptsA, "成功确认后不应再次重投")
	mu.Unlock()
}

// envelopeFixture is a CloudEvents-compatible envelope for MQ round-trip tests.
type envelopeFixture struct {
	ID              string `json:"id"`
	Source          string `json:"source"`
	SpecVersion     string `json:"specversion"`
	Type            string `json:"type"`
	Time            string `json:"time"`
	DataContentType string `json:"datacontenttype"`
	Data            string `json:"data"`
	TraceID         string `json:"traceid"`
	IdempotencyKey  string `json:"idempotencykey"`
	ShardKey        string `json:"shardkey"`
}

// runEventEnvelopeMQRoundtrip verifies that a CloudEvents-compatible envelope can
// be serialised, published, consumed, and deserialised through an MQ provider.
func runEventEnvelopeMQRoundtrip(t *testing.T, p mq.MQProvider) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, p.Connect(ctx))
	t.Cleanup(func() { _ = p.Close() })

	subject := fmt.Sprintf("core.event.test.%d", time.Now().UnixNano())

	env := envelopeFixture{
		ID:              "test-id-1",
		Source:          "integration-test",
		SpecVersion:     "1.0",
		Type:            "com.example.test",
		Time:            time.Now().UTC().Format(time.RFC3339),
		DataContentType: "application/json",
		Data:            `{"hello":"world"}`,
		TraceID:         "trace-abc",
		IdempotencyKey:  "idem-001",
		ShardKey:        "shard-1",
	}
	rawEnv, err := json.Marshal(env)
	require.NoError(t, err)

	received := make(chan []byte, 1)
	cancel, err := p.Subscribe(ctx, subject, func(msg *mq.Message) {
		b := make([]byte, len(msg.Data))
		copy(b, msg.Data)
		received <- b
		if msg.Ack != nil {
			_ = msg.Ack()
		}
	})
	require.NoError(t, err)
	defer cancel()

	require.NoError(t, p.Publish(ctx, subject, rawEnv, nil))

	select {
	case got := <-received:
		var decoded envelopeFixture
		require.NoError(t, json.Unmarshal(got, &decoded), "received bytes should be a valid JSON envelope")
		assert.Equal(t, env.ID, decoded.ID)
		assert.Equal(t, env.Source, decoded.Source)
		assert.Equal(t, env.SpecVersion, decoded.SpecVersion)
		assert.Equal(t, env.TraceID, decoded.TraceID)
		assert.Equal(t, env.IdempotencyKey, decoded.IdempotencyKey)
		assert.Equal(t, env.ShardKey, decoded.ShardKey)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for envelope delivery")
	}
}

// TestMQEventStreamRedis verifies event envelope round-trip through Redis Streams.
// Requires CORE_TEST_REDIS_STREAM=1.
func TestMQEventStreamRedis(t *testing.T) {
	if os.Getenv("CORE_TEST_REDIS_STREAM") == "" {
		t.Skip("CORE_TEST_REDIS_STREAM not set")
	}
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	p := mq.NewRedisStreamProvider(addr, "core-event-int", 0)
	runEventEnvelopeMQRoundtrip(t, p)
}

// TestMQEventStreamNATS verifies event envelope round-trip through NATS JetStream.
// Requires CORE_TEST_NATS=1.
func TestMQEventStreamNATS(t *testing.T) {
	if os.Getenv("CORE_TEST_NATS") == "" {
		t.Skip("CORE_TEST_NATS not set")
	}
	url := os.Getenv("CORE_TEST_NATS_URL")
	if url == "" {
		url = "nats://127.0.0.1:4222"
	}
	p := mq.NewNATSJetStreamProvider(url, "core-event-int", "core-event-int")
	runEventEnvelopeMQRoundtrip(t, p)
}

// TestMQEventBridgeKafka 验证真实 Kafka 上的 MQBridge Envelope round-trip。
func TestMQEventBridgeKafka(t *testing.T) {
	if os.Getenv("CORE_TEST_KAFKA") == "" {
		t.Skip("CORE_TEST_KAFKA not set")
	}
	runEventBridgeMQRoundtrip(t, mq.NewKafkaProvider(config.KafkaMQConfig{
		Brokers: kafkaTestBrokers(), Prefix: "core-event-bridge", ClientID: "core-event-bridge", StartOffset: "earliest",
	}))
}

// TestMQEventBridgeRabbitMQ 验证真实 RabbitMQ 上的 MQBridge Envelope round-trip。
func TestMQEventBridgeRabbitMQ(t *testing.T) {
	if os.Getenv("CORE_TEST_RABBITMQ") == "" {
		t.Skip("CORE_TEST_RABBITMQ not set")
	}
	runEventBridgeMQRoundtrip(t, mq.NewRabbitMQProvider(config.RabbitMQConfig{
		URL: rabbitMQTestURL(), Exchange: "core.event.bridge", QueuePrefix: "core-event-bridge", Prefetch: 1,
	}))
}

func runEventBridgeMQRoundtrip(t *testing.T, provider mq.MQProvider) {
	t.Helper()
	ctx, cancelContext := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelContext()
	require.NoError(t, provider.Connect(ctx))
	manager := mq.NewManager()
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent(provider.Name()))
	t.Cleanup(func() { _ = manager.Close() })

	stream := event.NewStream()
	bridge := event.NewMQBridge(stream, manager)
	subject := fmt.Sprintf("core.event.bridge.%d", time.Now().UnixNano())
	eventType := "order.changed"
	received := make(chan *event.Envelope, 1)
	cancelLocal, err := stream.SubscribeControl(eventType, func(envelope *event.Envelope) error {
		received <- envelope
		return nil
	})
	require.NoError(t, err)
	defer cancelLocal()
	cancelExternal, err := bridge.SubscribeReliable(ctx, subject, "integration-service")
	require.NoError(t, err)
	defer cancelExternal()

	envelope := event.NewEnvelope("order-service", eventType, []byte(`{"orderID":"42"}`))
	envelope.Subject = subject
	envelope.TraceID = "trace-42"
	envelope.IdempotencyKey = "event-42"
	envelope.ShardKey = "order-42"
	require.NoError(t, bridge.Publish(ctx, subject, envelope))
	select {
	case actual := <-received:
		assert.Equal(t, envelope.ID, actual.ID)
		assert.Equal(t, envelope.Type, actual.Type)
		assert.Equal(t, envelope.Subject, actual.Subject)
		assert.Equal(t, envelope.TraceID, actual.TraceID)
		assert.Equal(t, envelope.IdempotencyKey, actual.IdempotencyKey)
		assert.Equal(t, envelope.ShardKey, actual.ShardKey)
	case <-ctx.Done():
		t.Fatalf("等待 EventBridge round-trip 超时: %v", ctx.Err())
	}
}

func kafkaTestBrokers() []string {
	brokers := os.Getenv("CORE_TEST_KAFKA_BROKERS")
	if brokers == "" {
		brokers = "127.0.0.1:9092"
	}
	return []string{brokers}
}

func rabbitMQTestURL() string {
	rawURL := os.Getenv("CORE_TEST_RABBITMQ_URL")
	if rawURL == "" {
		return "amqp://core:core_test_password@127.0.0.1:5672/"
	}
	return rawURL
}
