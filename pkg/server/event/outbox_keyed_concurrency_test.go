package event_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/require"
)

type blockingOutboxExternal struct {
	started chan string
	release chan struct{}

	mu       sync.Mutex
	inflight map[string]int
	peak     map[string]int
}

func newBlockingOutboxExternal() *blockingOutboxExternal {
	return &blockingOutboxExternal{
		started:  make(chan string, 8),
		release:  make(chan struct{}),
		inflight: make(map[string]int),
		peak:     make(map[string]int),
	}
}

func (e *blockingOutboxExternal) Publish(ctx context.Context, _ string, env *event.Envelope) error {
	e.mu.Lock()
	e.inflight[env.ShardKey]++
	if e.inflight[env.ShardKey] > e.peak[env.ShardKey] {
		e.peak[env.ShardKey] = e.inflight[env.ShardKey]
	}
	e.mu.Unlock()

	e.started <- env.ID
	select {
	case <-e.release:
	case <-ctx.Done():
		return ctx.Err()
	}

	e.mu.Lock()
	e.inflight[env.ShardKey]--
	e.mu.Unlock()
	return nil
}

func TestOutboxKeyConcurrencyRunsDifferentKeysInParallelAndSameKeySerially(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{SubscriberID: "svc"})
	external := newBlockingOutboxExternal()
	bridge.SetExternalPublisher(external)
	store := &barrierStore{pending: []event.OutboxMessage{
		{EventID: "a1", EventType: "fill", Subject: "fills", ShardKey: "a", Payload: []byte("a1")},
		{EventID: "a2", EventType: "fill", Subject: "fills", ShardKey: "a", Payload: []byte("a2")},
		{EventID: "b1", EventType: "fill", Subject: "fills", ShardKey: "b", Payload: []byte("b1")},
	}}
	require.NoError(t, bridge.UseOutbox(event.OutboxOptions{
		SourceService:  "trades",
		Store:          store,
		Interval:       time.Hour,
		BatchSize:      10,
		External:       true,
		KeyConcurrency: 2,
	}))
	defer func() {
		select {
		case <-external.release:
		default:
			close(external.release)
		}
		require.NoError(t, bridge.Close(context.Background()))
	}()

	bridge.NotifyOutbox()
	started := map[string]bool{}
	for len(started) < 2 {
		select {
		case id := <-external.started:
			started[id] = true
		case <-time.After(time.Second):
			t.Fatalf("different keys did not start concurrently: %v", started)
		}
	}
	require.True(t, started["a1"])
	require.True(t, started["b1"])
	require.False(t, started["a2"])

	external.mu.Lock()
	require.Equal(t, 1, external.peak["a"])
	external.mu.Unlock()
	close(external.release)

	select {
	case id := <-external.started:
		require.Equal(t, "a2", id)
	case <-time.After(time.Second):
		t.Fatal("same-key successor did not resume after predecessor completed")
	}
}

func TestOutboxZeroKeyConcurrencyKeepsSerialDefault(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{SubscriberID: "svc"})
	external := newBlockingOutboxExternal()
	bridge.SetExternalPublisher(external)
	store := &barrierStore{pending: []event.OutboxMessage{
		{EventID: "a1", EventType: "fill", Subject: "fills", ShardKey: "a", Payload: []byte("a1")},
		{EventID: "b1", EventType: "fill", Subject: "fills", ShardKey: "b", Payload: []byte("b1")},
	}}
	require.NoError(t, bridge.UseOutbox(event.OutboxOptions{
		SourceService: "trades", Store: store, Interval: time.Hour, BatchSize: 10, External: true,
	}))
	defer func() {
		select {
		case <-external.release:
		default:
			close(external.release)
		}
		require.NoError(t, bridge.Close(context.Background()))
	}()

	bridge.NotifyOutbox()
	select {
	case <-external.started:
	case <-time.After(time.Second):
		t.Fatal("first publish did not start")
	}
	select {
	case id := <-external.started:
		t.Fatalf("zero-value concurrency must remain serial, unexpectedly started %s", id)
	case <-time.After(100 * time.Millisecond):
	}
}
