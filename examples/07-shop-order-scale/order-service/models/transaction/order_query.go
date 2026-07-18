// Package transaction 提供 07 订单服务远程权威订单查询和状态更新能力。
package transaction

import (
	"errors"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// OrderQueryFilter 定义远程权威订单分页查询条件。
type OrderQueryFilter struct {
	UserID     uint
	SupplierID uint
}

// ListRemoteOrdersWith 从远程权威库分页查询订单。
func ListRemoteOrdersWith(action persistencetypes.IDataAction, filter OrderQueryFilter, page, size int) ([]*Order, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 1000 {
		size = 100
	}
	var items []*Order
	query := store.NewSearch(NewOrder(), size)
	query.Page = page
	if filter.UserID > 0 {
		query.AddWhereN("UserID", filter.UserID)
	}
	if filter.SupplierID > 0 {
		query.AddWhereN("SupplierID", filter.SupplierID)
	}
	query.AddSortN("ID", false)
	err := action.Load(query, &items)
	return items, int64(len(items)), err
}

// CancelRemoteOrderWith 在远程权威库撤销订单。
func CancelRemoteOrderWith(action persistencetypes.IDataAction, orderID, userID uint) (*Order, error) {
	order, err := findRemoteOrderByIDWith(action, orderID)
	if err != nil {
		return nil, err
	}
	if userID > 0 && order.UserID != userID {
		return nil, errors.New("无权撤销该订单")
	}
	order.OrderStatus = OrderStatusCancelled
	order.OrderRevision++
	return order, order.UpdateWith(action)
}

// PayRemoteOrderWith 在远程权威库标记订单支付成功。
func PayRemoteOrderWith(action persistencetypes.IDataAction, orderID, userID uint, paymentID string) (*Order, error) {
	order, err := findRemoteOrderByIDWith(action, orderID)
	if err != nil {
		return nil, err
	}
	if userID > 0 && order.UserID != userID {
		return nil, errors.New("无权支付该订单")
	}
	now := time.Now().UTC()
	order.PaymentStatus = PaymentStatusPaid
	order.CurrentPaymentID = paymentID
	order.OrderRevision++
	order.SyncedAt = &now
	return order, order.UpdateWith(action)
}

func findRemoteOrderByIDWith(action persistencetypes.IDataAction, orderID uint) (*Order, error) {
	var items []*Order
	query := store.NewSearch(NewOrder(), 1)
	query.AddWhereN("ID", orderID)
	if err := action.Load(query, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("远程订单不存在")
	}
	return items[0], nil
}
