// Package business 提供 07 订单服务远程权威订单查询能力。
package business

import (
	"context"
	"sort"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
)

// ListOrders 从共享远程权威库读取订单列表。
func ListOrders(orders models.OrderWriteAccess, filter models.OrderQueryFilter, page, size int) ([]*models.Order, int64, error) {
	var items []*models.Order
	var total int64
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		items, total, err = models.ListRemoteOrdersWith(action, filter, page, size)
		return err
	})
	if err == nil && filter.UserID > 0 && filter.SupplierID == 0 {
		if pending, pendingErr := orders.PendingByUser(context.Background(), filter.UserID); pendingErr == nil && len(pending) > 0 {
			items = mergeOrders(items, pending)
			total = int64(len(items))
		}
	}
	return items, total, err
}

// CancelOrder 在远程权威库撤销订单。
func CancelOrder(orders models.OrderWriteAccess, orderID, userID uint, traceID string) (*models.Order, error) {
	var order *models.Order
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		order, err = models.CancelRemoteOrderWith(action, orderID, userID, traceID)
		if err != nil {
			return err
		}
		return writeOrderChangedOutboxWith(action, order, contract.EventOrderStatusChanged)
	})
	if err != nil {
		_, _ = (RemoteOrderSyncer{Store: orders}).DrainOnce(context.Background(), 100)
		err = models.RunRemoteTransaction(func(action models.DataAction) error {
			var retryErr error
			order, retryErr = models.CancelRemoteOrderWith(action, orderID, userID, traceID)
			if retryErr != nil {
				return retryErr
			}
			return writeOrderChangedOutboxWith(action, order, contract.EventOrderStatusChanged)
		})
	}
	return order, err
}

// PayOrder 在远程权威库标记订单支付成功。
func PayOrder(orders models.OrderWriteAccess, orderID, userID uint, paymentID, traceID string) (*models.Order, error) {
	var order *models.Order
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		order, err = models.PayRemoteOrderWith(action, orderID, userID, paymentID, traceID)
		if err != nil {
			return err
		}
		return writeOrderChangedOutboxWith(action, order, contract.EventPaymentChanged)
	})
	if err != nil {
		_, _ = (RemoteOrderSyncer{Store: orders}).DrainOnce(context.Background(), 100)
		err = models.RunRemoteTransaction(func(action models.DataAction) error {
			var retryErr error
			order, retryErr = models.PayRemoteOrderWith(action, orderID, userID, paymentID, traceID)
			if retryErr != nil {
				return retryErr
			}
			return writeOrderChangedOutboxWith(action, order, contract.EventPaymentChanged)
		})
	}
	return order, err
}

func writeOrderChangedOutboxWith(action models.DataAction, order *models.Order, eventType string) error {
	eventID := orderEventID(order.ID, eventType)
	outbox, err := models.NewOutboxRecord(order.TraceID, eventID, eventType, contract.SubjectOrderChanged, BuildOrderChangedEvent(order, eventID, eventType))
	if err != nil {
		return err
	}
	outbox.ServiceName = order.ServiceName
	outbox.ServiceInstanceID = order.ServiceInstanceID
	outbox.ServiceInstanceIP = order.ServiceInstanceIP
	return models.InsertOutboxIfMissingWith(action, outbox)
}

func mergeOrders(remote []*models.Order, pending []*models.Order) []*models.Order {
	byID := make(map[uint]*models.Order, len(remote)+len(pending))
	for _, item := range remote {
		if item != nil {
			byID[item.ID] = item
		}
	}
	for _, item := range pending {
		if item != nil {
			byID[item.ID] = item
		}
	}
	result := make([]*models.Order, 0, len(byID))
	for _, item := range byID {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	return result
}
