// 本文件验证可选批量确认的成功前缀、失败恢复与分键并发契约。
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

// TestOutboxBatchMarkerConfirmsSuccessfulPrefixOnce 验证发布失败不越过同 key 屏障。
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

// TestOutboxBatchMarkerFailureLeavesPrefixUnpublished 验证确认失败保留 pending，重启后用原 EventID 重试。
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
	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.batchCalls == 1
	}, time.Second, time.Millisecond)
	require.NoError(t, bridge.Close(context.Background()))

	store.mu.Lock()
	require.Equal(t, 1, store.batchCalls)
	require.Empty(t, store.marked)
	require.Equal(t, []string{"a1", "b1"}, pendingIDs(store.pending))
	store.batchErr = nil
	store.mu.Unlock()

	restarted := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{SubscriberID: "svc"})
	t.Cleanup(func() { require.NoError(t, restarted.Close(context.Background())) })
	restarted.SetExternalPublisher(external)
	require.NoError(t, restarted.UseOutbox(event.OutboxOptions{
		SourceService: "trades", Store: store, Interval: time.Hour, BatchSize: 10, External: true,
	}))
	restarted.NotifyOutbox()
	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.batchCalls == 2 && len(store.pending) == 0 && store.singleCalls == 0
	}, time.Second, time.Millisecond)
	external.mu.Lock()
	defer external.mu.Unlock()
	require.Equal(t, []string{"a1", "b1", "a1", "b1"}, external.published)
}

// TestOutboxBatchMarkerKeyedConcurrencyConfirmsOnce 验证多个有序 lane 共用一次批量确认。
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
