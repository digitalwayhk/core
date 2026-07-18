// Package transaction 将 07 订单服务 Outbox 表适配给标准 EventBridge。
package transaction

import (
	"context"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/event"
)

// OutboxStore 将本地 Outbox 表暴露给 ServiceEventBridge。
type OutboxStore struct{}

// LoadPending 读取等待发布的事件批次。
func (OutboxStore) LoadPending(_ context.Context, limit int) ([]event.OutboxMessage, error) {
	items, err := PendingOutbox(limit)
	if err != nil {
		return nil, err
	}
	result := make([]event.OutboxMessage, 0, len(items))
	for _, item := range items {
		result = append(result, event.OutboxMessage{
			ID: item.ID, EventID: item.EventID, EventType: item.EventType,
			Subject: item.Subject, Payload: item.Payload, TraceID: item.TraceID,
		})
	}
	return result, nil
}

// MarkPublished 标记指定事件已经发布。
func (OutboxStore) MarkPublished(_ context.Context, message event.OutboxMessage) error {
	return store.RunLocalTransaction(func() error { return nil }, func(action persistencetypes.IDataAction) error {
		items, err := PendingOutbox(100)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.ID == message.ID {
				return MarkOutboxPublishedWith(action, item)
			}
		}
		return nil
	})
}
