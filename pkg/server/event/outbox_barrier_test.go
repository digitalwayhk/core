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

// LoadPendingSkipping 跳过 blocked ordering keys，保证其他 key 仍可推进。
func (s *barrierStore) LoadPendingSkipping(_ context.Context, limit int, skipOrderingKeys []string) ([]event.OutboxMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	skip := make(map[string]struct{}, len(skipOrderingKeys))
	for _, k := range skipOrderingKeys {
		skip[k] = struct{}{}
	}
	out := make([]event.OutboxMessage, 0, limit)
	for _, item := range s.pending {
		key := item.ShardKey
		if key == "" {
			key = item.EventType + ":" + item.EventID
		}
		if _, blocked := skip[key]; blocked {
			continue
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
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

func TestOutboxHotKeyDoesNotStarveOtherKeysWithSkipStore(t *testing.T) {
	stream := event.NewStream()
	bridge := event.NewServiceEventBridge(stream, event.ServiceEventBridgeOptions{SubscriberID: "svc"})
	external := &barrierExternal{failID: "a1"}
	bridge.SetExternalPublisher(external)
	// batchSize=2：hot key a 的两条占满首批；无 Skip 时 b1 永远进不了批。
	store := &barrierStore{pending: []event.OutboxMessage{
		{EventID: "a1", EventType: "fill", Subject: "fills", ShardKey: "market-a", Payload: []byte("a1")},
		{EventID: "a2", EventType: "fill", Subject: "fills", ShardKey: "market-a", Payload: []byte("a2")},
		{EventID: "b1", EventType: "fill", Subject: "fills", ShardKey: "market-b", Payload: []byte("b1")},
	}}
	require.NoError(t, bridge.UseOutbox(event.OutboxOptions{
		SourceService: "trades",
		Store:         store,
		Interval:      time.Hour,
		BatchSize:     2,
		External:      true,
	}))
	bridge.NotifyOutbox()
	time.Sleep(150 * time.Millisecond)

	external.mu.Lock()
	published := append([]string(nil), external.published...)
	external.mu.Unlock()
	require.Equal(t, []string{"b1"}, published)
}

func TestOutboxBarrierSurvivesPublisherRestart(t *testing.T) {
	stream := event.NewStream()
	external := &barrierExternal{failID: "a2"}
	store := &barrierStore{pending: []event.OutboxMessage{
		{EventID: "a1", EventType: "fill", Subject: "fills", ShardKey: "market-a", Payload: []byte("a1")},
		{EventID: "a2", EventType: "fill", Subject: "fills", ShardKey: "market-a", Payload: []byte("a2")},
		{EventID: "a3", EventType: "fill", Subject: "fills", ShardKey: "market-a", Payload: []byte("a3")},
		{EventID: "b1", EventType: "fill", Subject: "fills", ShardKey: "market-b", Payload: []byte("b1")},
	}}

	bridge1 := event.NewServiceEventBridge(stream, event.ServiceEventBridgeOptions{SubscriberID: "svc"})
	bridge1.SetExternalPublisher(external)
	require.NoError(t, bridge1.UseOutbox(event.OutboxOptions{
		SourceService: "trades", Store: store, Interval: time.Hour, BatchSize: 10, External: true,
	}))
	bridge1.NotifyOutbox()
	time.Sleep(150 * time.Millisecond)
	require.NoError(t, bridge1.Close(context.Background()))

	// 新 publisher 实例：a2 仍失败，a3 不得越过；b1 可继续。
	bridge2 := event.NewServiceEventBridge(stream, event.ServiceEventBridgeOptions{SubscriberID: "svc"})
	bridge2.SetExternalPublisher(external)
	require.NoError(t, bridge2.UseOutbox(event.OutboxOptions{
		SourceService: "trades", Store: store, Interval: time.Hour, BatchSize: 10, External: true,
	}))
	bridge2.NotifyOutbox()
	time.Sleep(150 * time.Millisecond)

	external.mu.Lock()
	published := append([]string(nil), external.published...)
	external.mu.Unlock()
	require.Equal(t, []string{"a1", "b1"}, published)
	// a3 仍 unpublished
	store.mu.Lock()
	var remaining []string
	for _, item := range store.pending {
		remaining = append(remaining, item.EventID)
	}
	store.mu.Unlock()
	require.Contains(t, remaining, "a2")
	require.Contains(t, remaining, "a3")
}
