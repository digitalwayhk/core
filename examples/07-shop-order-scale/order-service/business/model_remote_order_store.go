// Package business 提供 07 订单服务同步器默认远程权威库适配器。
package business

import (
	"context"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
)

// ModelRemoteOrderStore 使用 models 远程事务实现订单 upsert。
type ModelRemoteOrderStore struct{}

// UpsertBatch 在一个 MySQL 事务内批量写入订单事实及对应 Outbox。
func (ModelRemoteOrderStore) UpsertBatch(_ context.Context, orders []*models.Order) ([]*models.Order, error) {
	var stored []*models.Order
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		stored, err = models.UpsertRemoteOrdersWith(action, orders)
		if err != nil {
			return err
		}
		outboxes := make([]*models.OutboxRecord, 0, len(stored))
		for _, order := range stored {
			outbox, buildErr := newOrderCreatedOutbox(order)
			if buildErr != nil {
				return buildErr
			}
			outbox.ServiceName = order.ServiceName
			outbox.ServiceInstanceID = order.ServiceInstanceID
			outbox.ServiceInstanceIP = order.ServiceInstanceIP
			outboxes = append(outboxes, outbox)
		}
		return models.InsertOutboxesIfMissingWith(action, outboxes)
	})
	return stored, err
}

// Upsert 保留单订单调用能力，内部复用批量事务语义。
func (store ModelRemoteOrderStore) Upsert(ctx context.Context, order *models.Order) (*models.Order, error) {
	stored, err := store.UpsertBatch(ctx, []*models.Order{order})
	if err != nil || len(stored) == 0 {
		return nil, err
	}
	return stored[0], nil
}
