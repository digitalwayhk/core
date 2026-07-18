// Package business 实现 07 订单服务从本地 pending 到远程权威库的同步能力。
package business

import (
	"context"
	"encoding/json"

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

// DrainOnce 尝试同步一批本地 pending，失败只标记 pending 并保留重试。
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

func (s RemoteOrderSyncer) syncOne(ctx context.Context, remote RemoteOrderStore, pending *models.LocalPendingOrder) error {
	order := models.NewOrder()
	if err := json.Unmarshal(pending.Payload, order); err != nil {
		return markPendingFailed(pending, err)
	}
	stored, err := remote.Upsert(ctx, order)
	if err != nil {
		return markPendingFailed(pending, err)
	}
	return models.RunLocalTransaction(func(action models.DataAction) error {
		if err := models.MarkPendingSyncedWith(action, pending); err != nil {
			return err
		}
		outbox, err := models.NewOutboxRecord(stored.TraceID, stored.RequestID, contract.EventOrderCreated, contract.SubjectOrderChanged, BuildOrderChangedEvent(stored, stored.RequestID, contract.EventOrderCreated))
		if err != nil {
			return err
		}
		outbox.ServiceName = stored.ServiceName
		outbox.ServiceInstanceID = stored.ServiceInstanceID
		return outbox.InsertWith(action)
	})
}

func markPendingFailed(pending *models.LocalPendingOrder, err error) error {
	return models.RunLocalTransaction(func(action models.DataAction) error {
		return models.MarkPendingFailedWith(action, pending, err.Error())
	})
}
