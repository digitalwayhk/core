// 本文件定义当前服务交易事实、Outbox、Inbox 或投影模型能力。
package transaction

import (
	"context"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/internal/store"
	"github.com/digitalwayhk/core/pkg/server/event"
)

// OutboxStore 将 Supplier Service 本地 Outbox 表适配给框架事件发布器。
type OutboxStore struct{}

// LoadPending 实现本类型在当前服务边界中的行为。
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
				Subject: item.Subject, Payload: item.Payload, TraceID: item.TraceID,
			})
		}
		return nil
	})
	return result, err
}

// MarkPublished 实现本类型在当前服务边界中的行为。
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
