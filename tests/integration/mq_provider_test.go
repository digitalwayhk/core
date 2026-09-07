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
// To run all MQ integration tests:
//
// CORE_TEST_REDIS_STREAM=1 CORE_TEST_NATS=1 go test -tags=integration ./tests/integration/ -run TestMQ
package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

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

// TestMQLifecycleRedis 对真实 Redis 执行统一的多组/pending/离线组/安全回收契约。
func TestMQLifecycleRedis(t *testing.T) {
	if os.Getenv("CORE_TEST_REDIS_STREAM") == "" {
		t.Skip("CORE_TEST_REDIS_STREAM not set")
	}
	addr := envOrDefault("CORE_TEST_REDIS_ADDR", "127.0.0.1:6379")
	prefix := fmt.Sprintf("core:integration:lifecycle:%d", time.Now().UnixNano())
	provider := mq.NewRedisStreamProvider(addr, prefix, 0)
	runMQLifecycleContract(t, provider, "fills")
}

// TestMQLifecycleNATS 对真实 NATS JetStream 执行与 Redis 相同的生命周期契约。
func TestMQLifecycleNATS(t *testing.T) {
	if os.Getenv("CORE_TEST_NATS") == "" {
		t.Skip("CORE_TEST_NATS not set")
	}
	url := envOrDefault("CORE_TEST_NATS_URL", "nats://127.0.0.1:4222")
	prefix := fmt.Sprintf("coreint%d", time.Now().UnixNano())
	provider := mq.NewNATSJetStreamProvider(url, prefix, prefix)
	runMQLifecycleContract(t, provider, "fills")
}

func runMQLifecycleContract(t *testing.T, provider mq.LifecycleConformanceProvider, subject string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	require.NoError(t, provider.Connect(ctx))
	t.Cleanup(func() { _ = provider.Close() })
	require.NoError(t, mq.VerifyMessageLifecycleConformance(ctx, provider, subject))
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

const (
	restartSubject = "lifecycle.restart"
	restartGroup   = "restart-required"
)

// TestMQRedisLifecycleBrokerRestartPrepare 在 Broker 重启前留下尚未 ACK 的 Redis 消息。
func TestMQRedisLifecycleBrokerRestartPrepare(t *testing.T) {
	token := requireRestartToken(t)
	provider := mq.NewRedisStreamProvider(
		envOrDefault("CORE_TEST_REDIS_ADDR", "127.0.0.1:6379"),
		"core:integration:restart:"+token, 0,
	)
	runBrokerRestartPrepare(t, provider, restartPolicy(mq.PublishAckBrokerAccepted), token)
}

// TestMQRedisLifecycleBrokerRestartRecover 验证 Redis 重启后未完成消息仍可继续消费并回收。
func TestMQRedisLifecycleBrokerRestartRecover(t *testing.T) {
	token := requireRestartToken(t)
	provider := mq.NewRedisStreamProvider(
		envOrDefault("CORE_TEST_REDIS_ADDR", "127.0.0.1:6379"),
		"core:integration:restart:"+token, 0,
	)
	runBrokerRestartRecover(t, provider, restartPolicy(mq.PublishAckBrokerAccepted), token)
}

// TestMQNATSLifecycleBrokerRestartPrepare 在 Broker 重启前留下尚未 ACK 的 JetStream 消息。
func TestMQNATSLifecycleBrokerRestartPrepare(t *testing.T) {
	token := requireRestartToken(t)
	prefix := "coreintrestart" + token
	provider := mq.NewNATSJetStreamProvider(
		envOrDefault("CORE_TEST_NATS_URL", "nats://127.0.0.1:4222"), prefix, prefix,
	)
	runBrokerRestartPrepare(t, provider, restartPolicy(mq.PublishAckBrokerPersisted), token)
}

// TestMQNATSLifecycleBrokerRestartRecover 验证 NATS 重启后未完成消息仍可继续消费并回收。
func TestMQNATSLifecycleBrokerRestartRecover(t *testing.T) {
	token := requireRestartToken(t)
	prefix := "coreintrestart" + token
	provider := mq.NewNATSJetStreamProvider(
		envOrDefault("CORE_TEST_NATS_URL", "nats://127.0.0.1:4222"), prefix, prefix,
	)
	runBrokerRestartRecover(t, provider, restartPolicy(mq.PublishAckBrokerPersisted), token)
}

func requireRestartToken(t *testing.T) string {
	t.Helper()
	token := strings.TrimSpace(os.Getenv("CORE_TEST_MQ_RESTART_TOKEN"))
	if token == "" {
		t.Skip("NOT RUN: Broker restart phase is orchestrated by test-external-integration.sh")
	}
	return token
}

func restartPolicy(publishAck mq.PublishAckLevel) mq.LifecyclePolicy {
	return mq.LifecyclePolicy{
		Subject: restartSubject, Mode: mq.LifecycleModeEnforce, RequiredPublishAck: publishAck,
		RequiredGroups: []mq.ConsumerGroupRequirement{{Name: restartGroup, Start: mq.StartFromAllRetained}},
		Retry: mq.RetryPolicy{
			MaxDeliveries: 5, DeadLetterSubject: restartSubject + ".dlq",
			Backoff: []time.Duration{2 * time.Second}, MaxAckPending: 10,
		},
		Reclaim: mq.ReclaimBudget{Interval: time.Hour, BatchSize: 10, TimeBudget: time.Second},
	}
}

func connectedLifecycleManager(
	t *testing.T,
	provider mq.LifecycleConformanceProvider,
	policy mq.LifecyclePolicy,
) (*mq.MQManager, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	require.NoError(t, provider.Connect(ctx))
	manager := mq.NewManager()
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent(provider.Name()))
	require.NoError(t, manager.RequireMessageLifecycle(ctx, policy))
	t.Cleanup(func() {
		_ = manager.Close()
		cancel()
	})
	return manager, ctx
}

func runBrokerRestartPrepare(
	t *testing.T,
	provider mq.LifecycleConformanceProvider,
	policy mq.LifecyclePolicy,
	token string,
) {
	t.Helper()
	manager, ctx := connectedLifecycleManager(t, provider, policy)
	attempted := make(chan struct{}, 1)
	cancel, err := manager.SubscribeReliable(ctx, policy.Subject, mq.ReliableSubscribeOptions{
		Group: restartGroup, Consumer: "before-restart", MinIdle: 50 * time.Millisecond,
		ClaimInterval: 20 * time.Millisecond,
	}, func(*mq.Message) error {
		select {
		case attempted <- struct{}{}:
		default:
		}
		return errors.New("intentional restart checkpoint")
	})
	require.NoError(t, err)
	defer cancel()
	require.NoError(t, manager.Publish(ctx, policy.Subject, []byte("restart-"+token), nil))
	select {
	case <-attempted:
	case <-ctx.Done():
		t.Fatal("message was not delivered before broker restart")
	}
	require.Eventually(t, func() bool {
		snapshot, inspectErr := provider.InspectLifecycle(ctx, policy)
		return inspectErr == nil && snapshot.PendingMessages != nil && *snapshot.PendingMessages > 0
	}, 5*time.Second, 20*time.Millisecond, "message must be pending before broker restart")
	prepareNoGroupRestart(t, ctx, provider)
}

func runBrokerRestartRecover(
	t *testing.T,
	provider mq.LifecycleConformanceProvider,
	policy mq.LifecyclePolicy,
	token string,
) {
	t.Helper()
	manager, ctx := connectedLifecycleManager(t, provider, policy)
	recovered := make(chan []byte, 1)
	cancel, err := manager.SubscribeReliable(ctx, policy.Subject, mq.ReliableSubscribeOptions{
		Group: restartGroup, Consumer: "after-restart", MinIdle: 50 * time.Millisecond,
		ClaimInterval: 20 * time.Millisecond,
	}, func(message *mq.Message) error {
		recovered <- append([]byte(nil), message.Data...)
		return nil
	})
	require.NoError(t, err)
	defer cancel()
	select {
	case payload := <-recovered:
		require.Equal(t, []byte("restart-"+token), payload)
	case <-ctx.Done():
		t.Fatal("pending message was not recovered after broker restart")
	}

	var snapshot mq.LifecycleSnapshot
	require.Eventually(t, func() bool {
		var inspectErr error
		snapshot, inspectErr = provider.InspectLifecycle(ctx, policy)
		return inspectErr == nil && snapshot.PendingMessages != nil && *snapshot.PendingMessages == 0 &&
			snapshot.SafeFrontier != "" && snapshot.SafeFrontier != "0" && snapshot.SafeFrontier != "0-0"
	}, 5*time.Second, 20*time.Millisecond)
	result, err := provider.ReclaimLifecycle(ctx, policy, snapshot)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Reclaimed)
	recoverNoGroupRestart(t, ctx, provider)
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
