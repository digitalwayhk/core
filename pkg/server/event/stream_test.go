package event_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStream_SubscribeAndPublish(t *testing.T) {
	s := event.NewStream()
	received := make(chan *event.Envelope, 1)

	cancel, err := s.Subscribe("test.event", func(env *event.Envelope) {
		received <- env
	})
	require.NoError(t, err)
	defer cancel()

	env := event.NewEnvelope("source", "test.event", []byte("data"))
	require.NoError(t, s.Publish(context.Background(), env))

	select {
	case got := <-received:
		assert.Equal(t, env.ID, got.ID)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestStream_MultipleSubscribers(t *testing.T) {
	s := event.NewStream()
	var mu sync.Mutex
	count := 0

	handler := func(_ *event.Envelope) {
		mu.Lock()
		count++
		mu.Unlock()
	}

	cancel1, err := s.Subscribe("multi.event", handler)
	require.NoError(t, err)
	defer cancel1()

	cancel2, err := s.Subscribe("multi.event", handler)
	require.NoError(t, err)
	defer cancel2()

	env := event.NewEnvelope("source", "multi.event", nil)
	require.NoError(t, s.Publish(context.Background(), env))

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	assert.Equal(t, 2, count)
	mu.Unlock()
}

func TestStream_CancelStopsDelivery(t *testing.T) {
	s := event.NewStream()
	received := make(chan struct{}, 5)

	cancel, err := s.Subscribe("cancel.event", func(_ *event.Envelope) {
		received <- struct{}{}
	})
	require.NoError(t, err)

	env := event.NewEnvelope("source", "cancel.event", nil)
	require.NoError(t, s.Publish(context.Background(), env))

	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("first delivery did not arrive")
	}

	cancel() // unsubscribe

	require.NoError(t, s.Publish(context.Background(), env))
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 0, len(received), "no message should arrive after cancel")
}

func TestStream_DifferentTypesNotDelivered(t *testing.T) {
	s := event.NewStream()
	received := make(chan *event.Envelope, 5)

	cancel, err := s.Subscribe("type.a", func(env *event.Envelope) {
		received <- env
	})
	require.NoError(t, err)
	defer cancel()

	envB := event.NewEnvelope("source", "type.b", nil)
	require.NoError(t, s.Publish(context.Background(), envB))

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 0, len(received), "type.b should not be delivered to type.a subscriber")
}
