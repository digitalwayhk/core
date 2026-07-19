// Package business 实现 07 订单服务从本地 pending 到远程权威库的同步能力。
package business

import (
	"context"
	"fmt"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
)

// OrderSyncStore 定义同步器所需的有界 pending 同步能力。
type OrderSyncStore interface {
	ForceSyncBatch(context.Context, int) (nosql.ForceSyncResult, error)
}

// RemoteOrderSyncer 将本地 pending 批量同步到远程 order 权威库。
type RemoteOrderSyncer struct {
	Store OrderSyncStore
}

// DrainOnce 触发一次 Badger pending 汇合；成功 key 由框架 ACK 并按模型策略清理。
func (s RemoteOrderSyncer) DrainOnce(ctx context.Context, limit int) (nosql.ForceSyncResult, error) {
	if err := ctx.Err(); err != nil {
		return nosql.ForceSyncResult{}, err
	}
	if s.Store == nil {
		return nosql.ForceSyncResult{}, models.ErrOrderWriteStoreUnavailable
	}
	return s.Store.ForceSyncBatch(ctx, limit)
}

func orderEventID(orderID uint, eventType string) string {
	return fmt.Sprintf("order:%d:%s", orderID, eventType)
}

func newOrderCreatedOutbox(order *models.Order) (*models.OutboxRecord, error) {
	eventID := orderEventID(order.ID, contract.EventOrderCreated)
	return models.NewOutboxRecord(order.TraceID, eventID, contract.EventOrderCreated, contract.SubjectOrderChanged, BuildOrderChangedEvent(order, eventID, contract.EventOrderCreated))
}
