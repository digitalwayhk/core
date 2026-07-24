package event_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/require"
)

type barrierStore struct {
	mu      sync.Mutex
	pending []event.OutboxMessage
	marked  []string
}

func (s *barrierStore) LoadPending(_ context.Context, limit int) ([]event.OutboxMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit > len(s.pending) {
		limit = len(s.pending)
	}
	out := make([]event.OutboxMessage, limit)
	copy(out, s.pending[:limit])
	return out, nil
}

func (s *barrierStore) MarkPublished(_ context.Context, message event.OutboxMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marked = append(s.marked, message.EventID)
	next := s.pending[:0]
	for _, item := range s.pending {
		if item.EventID != message.EventID {
			next = append(next, item)
		}
	}
	s.pending = next
	return nil
}

type barrierExternal struct {
	mu        sync.Mutex
	failID    string
	published []string
}

func (e *barrierExternal) Publish(_ context.Context, _ string, env *event.Envelope) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if env.ID == e.failID {
		return errors.New("publish failed")
	}
	e.published = append(e.published, env.ID)
	return nil
}

func TestOutboxSameKeyFailureBarrierAllowsOtherKeys(t *testing.T) {
	stream := event.NewStream()
	bridge := event.NewServiceEventBridge(stream, event.ServiceEventBridgeOptions{SubscriberID: "svc"})
	external := &barrierExternal{failID: "a2"}
	bridge.SetExternalPublisher(external)
	store := &barrierStore{pending: []event.OutboxMessage{
		{EventID: "a1", EventType: "fill", Subject: "fills", ShardKey: "market-a", Payload: []byte("a1")},
		{EventID: "a2", EventType: "fill", Subject: "fills", ShardKey: "market-a", Payload: []byte("a2")},
		{EventID: "a3", EventType: "fill", Subject: "fills", ShardKey: "market-a", Payload: []byte("a3")},
		{EventID: "b1", EventType: "fill", Subject: "fills", ShardKey: "market-b", Payload: []byte("b1")},
	}}
	require.NoError(t, bridge.UseOutbox(event.OutboxOptions{
		SourceService: "trades",
		Store:         store,
		Interval:      time.Hour,
		BatchSize:     10,
		External:      true,
	}))
	bridge.NotifyOutbox()
	time.Sleep(150 * time.Millisecond)

	external.mu.Lock()
	published := append([]string(nil), external.published...)
	external.mu.Unlock()
	require.Equal(t, []string{"a1", "b1"}, published)

	store.mu.Lock()
	marked := append([]string(nil), store.marked...)
	store.mu.Unlock()
	require.Equal(t, []string{"a1", "b1"}, marked)
}
