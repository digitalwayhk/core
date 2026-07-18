// Package transaction 提供 07 订单服务远程权威订单的持久化能力。
package transaction

import (
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// FindRemoteOrderByIdempotencyWith 按 UserID + requestID 查找远程权威订单。
func FindRemoteOrderByIdempotencyWith(action persistencetypes.IDataAction, userID uint, requestID string) (*Order, error) {
	var items []*Order
	query := store.NewSearch(NewOrder(), 1)
	query.AddWhereN("UserID", userID)
	query.AddWhereN("RequestID", strings.TrimSpace(requestID))
	if err := action.Load(query, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("远程订单不存在")
	}
	return items[0], nil
}

// UpsertRemoteOrderWith 将订单事实幂等写入远程权威库。
func UpsertRemoteOrderWith(action persistencetypes.IDataAction, order *Order) (*Order, error) {
	if order == nil {
		return nil, errors.New("订单不能为空")
	}
	existing, err := FindRemoteOrderByIdempotencyWith(action, order.UserID, order.RequestID)
	if err == nil && existing != nil {
		if existing.RequestFingerprint != order.RequestFingerprint {
			return nil, errors.New("幂等键已用于不同订单请求")
		}
		return existing, nil
	}
	now := time.Now().UTC()
	order.OrderStatus = OrderStatusSynced
	order.SyncedAt = &now
	if order.AcceptedAt.IsZero() {
		order.AcceptedAt = now
	}
	if err := order.InsertWith(action); err != nil {
		return nil, err
	}
	return order, nil
}
