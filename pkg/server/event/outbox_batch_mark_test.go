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

type batchMarkStore struct {
	mu          sync.Mutex
	pending     []event.OutboxMessage
	marked      []string
	batchCalls  int
	singleCalls int
	batchErr    error
}

func (s *batchMarkStore) LoadPending(_ context.Context, limit int) ([]event.OutboxMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit > len(s.pending) {
		limit = len(s.pending)
	}
	out := make([]event.OutboxMessage, limit)
	copy(out, s.pending[:limit])
	return out, nil
}

func (s *batchMarkStore) MarkPublished(_ context.Context, message event.OutboxMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.singleCalls++
	s.removePending(message.EventID)
	s.marked = append(s.marked, message.EventID)
	return nil
}

func (s *batchMarkStore) MarkPublishedBatch(_ context.Context, messages []event.OutboxMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchCalls++
	if s.batchErr != nil {
		return s.batchErr
	}
	if len(messages) == 0 {
		return errors.New("空批次不应调用 MarkPublishedBatch")
	}
	for _, message := range messages {
		s.removePending(message.EventID)
		s.marked = append(s.marked, message.EventID)
	}
	return nil
}

func (s *batchMarkStore) removePending(eventID string) {
	next := s.pending[:0]
	for _, item := range s.pending {
		if item.EventID != eventID {
			next = append(next, item)
		}
	}
	s.pending = next
}

func TestOutboxBatchMarkerConfirmsSuccessfulPrefixOnce(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{SubscriberID: "svc"})
	t.Cleanup(func() { require.NoError(t, bridge.Close(context.Background())) })
	external := &barrierExternal{failID: "a2"}
	bridge.SetExternalPublisher(external)
	store := &batchMarkStore{pending: []event.OutboxMessage{
		{EventID: "a1", EventType: "fill", Subject: "fills", ShardKey: "market-a", Payload: []byte("a1")},
		{EventID: "a2", EventType: "fill", Subject: "fills", ShardKey: "market-a", Payload: []byte("a2")},
		{EventID: "a3", EventType: "fill", Subject: "fills", ShardKey: "market-a", Payload: []byte("a3")},
		{EventID: "b1", EventType: "fill", Subject: "fills", ShardKey: "market-b", Payload: []byte("b1")},
	}}
	require.NoError(t, bridge.UseOutbox(event.OutboxOptions{
		SourceService: "trades", Store: store, Interval: time.Hour, BatchSize: 10, External: true,
	}))
	bridge.NotifyOutbox()

	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.batchCalls == 1 && len(store.marked) == 2
	}, time.Second, 10*time.Millisecond)

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Equal(t, 0, store.singleCalls)
	require.Equal(t, []string{"a1", "b1"}, store.marked)
	require.Equal(t, []string{"a2", "a3"}, pendingIDs(store.pending))
}

func TestOutboxBatchMarkerFailureLeavesPrefixUnpublished(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{SubscriberID: "svc"})
	t.Cleanup(func() { require.NoError(t, bridge.Close(context.Background())) })
	external := &barrierExternal{}
	bridge.SetExternalPublisher(external)
	store := &batchMarkStore{
		batchErr: errors.New("batch mark failed"),
		pending: []event.OutboxMessage{
			{EventID: "a1", EventType: "fill", Subject: "fills", ShardKey: "a", Payload: []byte("a1")},
			{EventID: "b1", EventType: "fill", Subject: "fills", ShardKey: "b", Payload: []byte("b1")},
		},
	}
	require.NoError(t, bridge.UseOutbox(event.OutboxOptions{
		SourceService: "trades", Store: store, Interval: time.Hour, BatchSize: 10, External: true,
	}))
	bridge.NotifyOutbox()
	time.Sleep(150 * time.Millisecond)

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Equal(t, 1, store.batchCalls)
	require.Empty(t, store.marked)
	require.Equal(t, []string{"a1", "b1"}, pendingIDs(store.pending))
}

func TestOutboxBatchMarkerKeyedConcurrencyConfirmsOnce(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{SubscriberID: "svc"})
	t.Cleanup(func() { require.NoError(t, bridge.Close(context.Background())) })
	external := &barrierExternal{}
	bridge.SetExternalPublisher(external)
	store := &batchMarkStore{pending: []event.OutboxMessage{
		{EventID: "a1", EventType: "fill", Subject: "fills", ShardKey: "a", Payload: []byte("a1")},
		{EventID: "a2", EventType: "fill", Subject: "fills", ShardKey: "a", Payload: []byte("a2")},
		{EventID: "b1", EventType: "fill", Subject: "fills", ShardKey: "b", Payload: []byte("b1")},
		{EventID: "b2", EventType: "fill", Subject: "fills", ShardKey: "b", Payload: []byte("b2")},
	}}
	require.NoError(t, bridge.UseOutbox(event.OutboxOptions{
		SourceService:  "trades",
		Store:          store,
		Interval:       time.Hour,
		BatchSize:      10,
		External:       true,
		KeyConcurrency: 2,
	}))
	bridge.NotifyOutbox()

	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.batchCalls == 1 && len(store.marked) == 4
	}, time.Second, 10*time.Millisecond)

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Equal(t, 0, store.singleCalls)
	require.ElementsMatch(t, []string{"a1", "a2", "b1", "b2"}, store.marked)
	require.Empty(t, store.pending)
}

func pendingIDs(items []event.OutboxMessage) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.EventID)
	}
	return out
}
