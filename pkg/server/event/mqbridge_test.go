package event_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMQProvider is an in-memory MQProvider that records published messages
// and synchronously delivers them to any registered subscriber handler.
// It is used to test MQBridge without requiring a real Redis or NATS server.
type fakeMQProvider struct {
	mu        sync.Mutex
	published []fakeMsg
	handler   func(msg *mq.Message)
	ackCount  int // incremented each time the Ack callback on a delivered message is called
}

type fakeMsg struct {
	subject string
	data    []byte
}

func (f *fakeMQProvider) Name() string                    { return "fake" }
func (f *fakeMQProvider) Connect(_ context.Context) error { return nil }
func (f *fakeMQProvider) Close() error                    { return nil }
func (f *fakeMQProvider) Health(_ context.Context) error  { return nil }

func (f *fakeMQProvider) Publish(_ context.Context, subject string, data []byte, _ *mq.PublishOptions) error {
	f.mu.Lock()
	f.published = append(f.published, fakeMsg{subject, data})
	h := f.handler
	f.mu.Unlock()

	if h != nil {
		h(&mq.Message{
			Subject: subject,
			Data:    data,
			Ack: func() error {
				f.mu.Lock()
				f.ackCount++
				f.mu.Unlock()
				return nil
			},
		})
	}
	return nil
}

func (f *fakeMQProvider) Subscribe(_ context.Context, _ string, handler func(*mq.Message)) (func(), error) {
	f.mu.Lock()
	f.handler = handler
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.handler = nil
		f.mu.Unlock()
	}, nil
}

// buildBridge wires up a Stream + fakeMQProvider + MQManager + MQBridge.
func buildBridge(t *testing.T) (*event.Stream, *fakeMQProvider, *event.MQBridge) {
	t.Helper()
	stream := event.NewStream()
	provider := &fakeMQProvider{}
	mgr := mq.NewManager()
	mgr.Register(provider)
	require.NoError(t, mgr.SetCurrent("fake"))
	bridge := event.NewMQBridge(stream, mgr)
	return stream, provider, bridge
}

// TestMQBridge_Publish_SerializesEnvelopeToMQ verifies that MQBridge.Publish
// serialises the Envelope to JSON and delivers it to the MQ provider.
func TestMQBridge_Publish_SerializesEnvelopeToMQ(t *testing.T) {
	_, provider, bridge := buildBridge(t)

	env := event.NewEnvelope("svc.test", "order.created", []byte(`{"id":1}`))
	env.TraceID = "trace-xyz"

	ctx := context.Background()
	require.NoError(t, bridge.Publish(ctx, "order.events", env))

	provider.mu.Lock()
	published := provider.published
	provider.mu.Unlock()

	require.Len(t, published, 1, "one message should be published to MQ")
	assert.Equal(t, "order.events", published[0].subject)

	var got event.Envelope
	require.NoError(t, json.Unmarshal(published[0].data, &got))
	assert.Equal(t, env.ID, got.ID)
	assert.Equal(t, env.Type, got.Type)
	assert.Equal(t, env.Source, got.Source)
	assert.Equal(t, env.TraceID, got.TraceID)
	assert.Equal(t, env.Data, got.Data)
}

// TestMQBridge_Subscribe_DeserializesAndDeliversToStream verifies that an MQ
// message received via the bridge is deserialised and forwarded to all
// Stream handlers registered for the corresponding event type.
func TestMQBridge_Subscribe_DeserializesAndDeliversToStream(t *testing.T) {
	stream, _, bridge := buildBridge(t)

	received := make(chan *event.Envelope, 1)
	cancel, err := stream.Subscribe("order.created", func(env *event.Envelope) {
		received <- env
	})
	require.NoError(t, err)
	defer cancel()

	ctx := context.Background()
	_, subErr := bridge.Subscribe(ctx, "order.events")
	require.NoError(t, subErr)

	env := event.NewEnvelope("svc.test", "order.created", []byte(`{"id":42}`))
	require.NoError(t, bridge.Publish(ctx, "order.events", env))

	select {
	case got := <-received:
		assert.Equal(t, env.ID, got.ID)
		assert.Equal(t, "order.created", got.Type)
		assert.Equal(t, env.Data, got.Data)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event delivery via MQBridge")
	}
}

// TestMQBridge_Roundtrip_EnvelopePreservesAllFields verifies that the full
// publish → MQ → subscribe → stream path preserves all Envelope fields,
// including IdempotencyKey and ShardKey.
func TestMQBridge_Roundtrip_EnvelopePreservesAllFields(t *testing.T) {
	stream, _, bridge := buildBridge(t)

	received := make(chan *event.Envelope, 1)
	cancel, err := stream.Subscribe("payment.settled", func(env *event.Envelope) {
		received <- env
	})
	require.NoError(t, err)
	defer cancel()

	ctx := context.Background()
	_, subErr := bridge.Subscribe(ctx, "payment.events")
	require.NoError(t, subErr)

	env := event.NewEnvelope("payment-svc", "payment.settled", []byte(`{"amount":100}`))
	env.Subject = "payment/ref-001"
	env.TraceID = "trace-abc"
	env.IdempotencyKey = "idem-key-123"
	env.ShardKey = "shard-7"

	require.NoError(t, bridge.Publish(ctx, "payment.events", env))

	select {
	case got := <-received:
		assert.Equal(t, env.ID, got.ID)
		assert.Equal(t, env.Source, got.Source)
		assert.Equal(t, env.Type, got.Type)
		assert.Equal(t, env.Subject, got.Subject)
		assert.Equal(t, env.TraceID, got.TraceID)
		assert.Equal(t, env.IdempotencyKey, got.IdempotencyKey)
		assert.Equal(t, env.ShardKey, got.ShardKey)
		assert.Equal(t, env.Data, got.Data)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for roundtrip envelope delivery")
	}
}

// TestMQBridge_Subscribe_AckAlwaysCalled verifies that the bridge calls
// msg.Ack() after delivering the event to the stream (fire-and-forget contract).
func TestMQBridge_Subscribe_AckAlwaysCalled(t *testing.T) {
	_, provider, bridge := buildBridge(t)

	ctx := context.Background()
	_, subErr := bridge.Subscribe(ctx, "ack.test.subject")
	require.NoError(t, subErr)

	env := event.NewEnvelope("svc", "ack.test", nil)
	require.NoError(t, bridge.Publish(ctx, "ack.test.subject", env))

	// Delivery is synchronous in fakeMQProvider; no sleep required.
	provider.mu.Lock()
	ackCount := provider.ackCount
	provider.mu.Unlock()
	assert.Equal(t, 1, ackCount, "Ack should be called exactly once per delivered message")
}
