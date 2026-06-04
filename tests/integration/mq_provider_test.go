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
	"fmt"
	"os"
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
