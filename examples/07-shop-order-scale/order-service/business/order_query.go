// Package business 提供 07 订单服务远程权威订单查询能力。
package business

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
)

// ListOrders 从共享远程权威库读取订单列表。
func ListOrders(filter models.OrderQueryFilter, page, size int) ([]*models.Order, int64, error) {
	var items []*models.Order
	var total int64
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		items, total, err = models.ListRemoteOrdersWith(action, filter, page, size)
		return err
	})
	return items, total, err
}

// CancelOrder 在远程权威库撤销订单。
func CancelOrder(orderID, userID uint, traceID string) (*models.Order, error) {
	var order *models.Order
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		order, err = models.CancelRemoteOrderWith(action, orderID, userID, traceID)
		return err
	})
	return order, err
}

// PayOrder 在远程权威库标记订单支付成功。
func PayOrder(orderID, userID uint, paymentID, traceID string) (*models.Order, error) {
	var order *models.Order
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		order, err = models.PayRemoteOrderWith(action, orderID, userID, paymentID, traceID)
		return err
	})
	return order, err
}
