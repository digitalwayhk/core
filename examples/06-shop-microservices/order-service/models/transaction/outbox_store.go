package transaction

import (
	"context"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/internal/store"
	"github.com/digitalwayhk/core/pkg/server/event"
)

// OutboxStore 将 Order Service 本地 Outbox 表适配给框架事件发布器。
type OutboxStore struct{}

func (OutboxStore) LoadPending(context.Context, int) ([]event.OutboxMessage, error) {
	var result []event.OutboxMessage
	err := store.RunSerialized(func() error {
		items, err := PendingOutbox()
		if err != nil {
			return err
		}
		result = make([]event.OutboxMessage, 0, len(items))
		for _, item := range items {
			result = append(result, event.OutboxMessage{
				ID: item.ID, EventID: item.EventID, EventType: item.EventType,
				Subject: item.Subject, Payload: item.Payload,
			})
		}
		return nil
	})
	return result, err
}

func (OutboxStore) MarkPublished(_ context.Context, message event.OutboxMessage) error {
	return store.RunSerialized(func() error {
		items, err := PendingOutbox()
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.ID == message.ID {
				return MarkOutboxPublished(item)
			}
		}
		return nil
	})
}
