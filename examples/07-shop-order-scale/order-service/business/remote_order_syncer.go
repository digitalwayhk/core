// Package business 实现 07 订单服务从本地 pending 到远程权威库的同步能力。
package business

import (
	"context"
	"fmt"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
)

// RemoteOrderStore 定义同步器写入远程权威库所需的最小接口。
type RemoteOrderStore interface {
	Upsert(context.Context, *models.Order) (*models.Order, error)
}

// RemoteOrderSyncer 将本地 pending 批量同步到远程 order 权威库。
type RemoteOrderSyncer struct {
	Remote RemoteOrderStore
}

// DrainOnce 尝试同步一批 Badger 本地订单，成功后删除本地副本。
func (s RemoteOrderSyncer) DrainOnce(ctx context.Context, limit int) error {
	remote := s.Remote
	if remote == nil {
		remote = ModelRemoteOrderStore{}
	}
	items, err := models.PendingLocalOrders(limit)
	if err != nil {
		return err
	}
	for _, pending := range items {
		if err := s.syncOne(ctx, remote, pending); err != nil {
			return err
		}
	}
	return nil
}

func (s RemoteOrderSyncer) syncOne(ctx context.Context, remote RemoteOrderStore, order *models.Order) error {
	_, err := remote.Upsert(ctx, order)
	if err != nil {
		return err
	}
	return models.RemoveLocalOrder(order)
}

func orderEventID(orderID uint, eventType string) string {
	return fmt.Sprintf("order:%d:%s", orderID, eventType)
}

func newOrderCreatedOutbox(order *models.Order) (*models.OutboxRecord, error) {
	eventID := orderEventID(order.ID, contract.EventOrderCreated)
	return models.NewOutboxRecord(order.TraceID, eventID, contract.EventOrderCreated, contract.SubjectOrderChanged, BuildOrderChangedEvent(order, eventID, contract.EventOrderCreated))
}
