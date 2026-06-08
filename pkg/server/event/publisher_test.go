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

// ============================================================
// helpers
// ============================================================

type capturingProvider struct {
	mu        sync.Mutex
	published [][]byte
	handler   func(*mq.Message)
}

func (c *capturingProvider) Name() string                    { return "capturing" }
func (c *capturingProvider) Connect(_ context.Context) error { return nil }
func (c *capturingProvider) Close() error                    { return nil }
func (c *capturingProvider) Health(_ context.Context) error  { return nil }
func (c *capturingProvider) Publish(_ context.Context, _ string, data []byte, _ *mq.PublishOptions) error {
	c.mu.Lock()
	c.published = append(c.published, data)
	h := c.handler
	c.mu.Unlock()
	if h != nil {
		h(&mq.Message{Subject: "test", Data: data, Ack: func() error { return nil }})
	}
	return nil
}
func (c *capturingProvider) Subscribe(_ context.Context, _ string, handler func(*mq.Message)) (func(), error) {
	c.mu.Lock()
	c.handler = handler
	c.mu.Unlock()
	return func() {}, nil
}

func buildPublisherWithBridge(t *testing.T) (*event.Stream, *capturingProvider, event.IPublisher) {
	t.Helper()
	stream := event.NewStream()
	prov := &capturingProvider{}
	mgr := mq.NewManager()
	mgr.Register(prov)
	require.NoError(t, mgr.SetCurrent("capturing"))
	bridge := event.NewMQBridge(stream, mgr)
	pub := event.NewPublisher(stream, bridge, "events")
	return stream, prov, pub
}

// ============================================================
// IPublisher interface compliance
// ============================================================

// TestPublisher_ImplementsIPublisher ensures *Publisher satisfies IPublisher at compile time.
func TestPublisher_ImplementsIPublisher(t *testing.T) {
	stream := event.NewStream()
	var _ event.IPublisher = event.NewPublisher(stream, nil, "")
}

// ============================================================
// Routing: bridge vs. stream
// ============================================================

// TestPublisher_RoutesToBridgeWhenAvailable verifies that when an MQBridge is
// configured the Publisher serialises and delivers via the MQ provider.
func TestPublisher_RoutesToBridgeWhenAvailable(t *testing.T) {
	_, prov, pub := buildPublisherWithBridge(t)

	env := event.NewEnvelope("svc.test", "order.shipped", []byte(`{"id":42}`))
	require.NoError(t, pub.Publish(context.Background(), env))

	prov.mu.Lock()
	published := prov.published
	prov.mu.Unlock()

	require.Len(t, published, 1, "bridge publish should forward to MQ provider")
	var got event.Envelope
	require.NoError(t, json.Unmarshal(published[0], &got))
	assert.Equal(t, env.ID, got.ID)
	assert.Equal(t, "order.shipped", got.Type)
}

// TestPublisher_RoutesToStreamWhenNoBridge verifies that without a bridge the
// Publisher delivers in-process to all Stream subscribers.
func TestPublisher_RoutesToStreamWhenNoBridge(t *testing.T) {
	stream := event.NewStream()
	pub := event.NewPublisher(stream, nil, "")

	received := make(chan *event.Envelope, 1)
	cancel, err := stream.Subscribe("local.event", func(env *event.Envelope) {
		received <- env
	})
	require.NoError(t, err)
	defer cancel()

	env := event.NewEnvelope("svc", "local.event", nil)
	require.NoError(t, pub.Publish(context.Background(), env))

	select {
	case got := <-received:
		assert.Equal(t, env.ID, got.ID)
	case <-time.After(time.Second):
		t.Fatal("in-process delivery timed out")
	}
}

// TestPublisher_NilStreamAndBridge_NoError ensures that a Publisher with
// neither stream nor bridge returns nil without panicking.
func TestPublisher_NilStreamAndBridge_NoError(t *testing.T) {
	pub := event.NewPublisher(nil, nil, "")
	env := event.NewEnvelope("svc", "noop.event", nil)
	assert.NoError(t, pub.Publish(context.Background(), env))
}

// TestPublisher_BridgeReceivesEnvelopeFields verifies that all Envelope fields
// survive the bridge round-trip (TraceID, IdempotencyKey, ShardKey).
func TestPublisher_BridgeReceivesEnvelopeFields(t *testing.T) {
	stream, prov, pub := buildPublisherWithBridge(t)

	received := make(chan *event.Envelope, 1)
	cancel, subErr := stream.Subscribe("payment.settled", func(env *event.Envelope) {
		received <- env
	})
	require.NoError(t, subErr)
	defer cancel()

	// Subscribe bridge to route incoming MQ messages back into the stream.
	mgr := mq.NewManager()
	mgr.Register(prov)
	require.NoError(t, mgr.SetCurrent("capturing"))
	bridge := event.NewMQBridge(stream, mgr)
	_, bridgeSubErr := bridge.Subscribe(context.Background(), "events")
	require.NoError(t, bridgeSubErr)

	env := event.NewEnvelope("payment-svc", "payment.settled", []byte(`{"amount":50}`))
	env.TraceID = "trace-001"
	env.IdempotencyKey = "idem-abc"
	env.ShardKey = "shard-3"
	require.NoError(t, pub.Publish(context.Background(), env))

	select {
	case got := <-received:
		assert.Equal(t, env.TraceID, got.TraceID)
		assert.Equal(t, env.IdempotencyKey, got.IdempotencyKey)
		assert.Equal(t, env.ShardKey, got.ShardKey)
	case <-time.After(2 * time.Second):
		t.Fatal("full roundtrip timed out")
	}
}
