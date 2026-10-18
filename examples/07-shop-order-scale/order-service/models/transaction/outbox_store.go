// Package transaction 将 07 订单服务 MySQL Outbox 表适配给标准 EventBridge。
package transaction

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	messagestore "github.com/digitalwayhk/core/pkg/persistence/message"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/event"
)

// OutboxStore 将 MySQL Outbox 表暴露给 ServiceEventBridge。
type OutboxStore struct{}

var (
	_ event.OutboxStore       = OutboxStore{}
	_ event.OutboxBatchMarker = OutboxStore{}
)

// LoadPending 读取等待发布的事件批次。
func (OutboxStore) LoadPending(_ context.Context, limit int) ([]event.OutboxMessage, error) {
	items, err := PendingOutbox(limit)
	if err != nil {
		return nil, err
	}
	result := make([]event.OutboxMessage, 0, len(items))
	for _, item := range items {
		key, err := orderEventKey(item.Payload)
		if err != nil {
			return nil, err
		}
		result = append(result, event.OutboxMessage{
			ID: item.ID, EventID: item.EventID, EventType: item.EventType,
			Subject: item.Subject, Payload: item.Payload, TraceID: item.TraceID, ShardKey: key,
		})
	}
	return result, nil
}

// MarkPublished 标记指定事件已经发布。
func (outbox OutboxStore) MarkPublished(ctx context.Context, message event.OutboxMessage) error {
	return outbox.MarkPublishedBatch(ctx, []event.OutboxMessage{message})
}

// MarkPublishedBatch 一次事务确认一批已发布事件。
func (OutboxStore) MarkPublishedBatch(ctx context.Context, messages []event.OutboxMessage) error {
	if len(messages) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	return messagestore.Run(ctx, store.GetRemote(), func(action persistencetypes.IDataAction) error {
		return markOutboxPublishedByIDs(ctx, action, ids)
	})
}

// orderEventKey 对创建、状态和支付事件统一按不可变的 OrderID 分键，不增加存储列。
func orderEventKey(data []byte) (string, error) {
	var payload struct {
		OrderID uint `json:"orderID"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	if payload.OrderID == 0 {
		return "", fmt.Errorf("订单事件缺少 OrderID")
	}
	return contract.OrderServiceName + ":order:" + strconv.FormatUint(uint64(payload.OrderID), 10), nil
}
