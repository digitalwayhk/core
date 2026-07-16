package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type flakyPublisher struct {
	mu    sync.Mutex
	calls int
}

func (p *flakyPublisher) Publish(context.Context, string, *event.Envelope) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls == 1 {
		return errors.New("临时发布失败")
	}
	return nil
}

func TestOutboxWorkerMarksOnlyAfterSuccessfulPublish(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})
	t.Cleanup(func() { require.NoError(t, bridge.Close(context.Background())) })
	publisher := &flakyPublisher{}
	bridge.SetExternalPublisher(publisher)
	record := OutboxRecord{ID: 1, EventID: "event-1", EventType: "order.changed", Subject: "orders", Payload: []byte(`{"id":1}`)}
	marked := make(chan struct{}, 1)
	var done bool
	var mu sync.Mutex
	worker := StartOutboxWorker(context.Background(), "orders", bridge, func() ([]OutboxRecord, error) {
		mu.Lock()
		defer mu.Unlock()
		if done {
			return nil, nil
		}
		return []OutboxRecord{record}, nil
	}, func(got OutboxRecord) error {
		assert.Equal(t, record.EventID, got.EventID)
		mu.Lock()
		done = true
		mu.Unlock()
		marked <- struct{}{}
		return nil
	})
	defer worker.Stop()
	select {
	case <-marked:
	case <-time.After(2 * time.Second):
		t.Fatal("Outbox 未在重试成功后标记")
	}
	publisher.mu.Lock()
	calls := publisher.calls
	publisher.mu.Unlock()
	assert.GreaterOrEqual(t, calls, 2)
}
