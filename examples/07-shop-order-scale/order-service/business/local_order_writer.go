// Package business 实现 07 订单服务本地可靠写入能力。
package business

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
)

// LocalOrderWriter 将订单请求先可靠写入当前实例本地 pending。
type LocalOrderWriter struct{}

// Accept 校验订单命令并在本地 pending 持久成功后返回订单 ID。
func (LocalOrderWriter) Accept(_ context.Context, command CreateOrderCommand) (uint, error) {
	if err := command.validate(); err != nil {
		return 0, err
	}
	order := orderFromCommand(command)
	payload, err := json.Marshal(order)
	if err != nil {
		return 0, err
	}
	pending := models.NewLocalPendingOrder()
	pending.ID = command.OrderID
	pending.OrderID = command.OrderID
	pending.UserID = command.UserID
	pending.RequestID = strings.TrimSpace(command.RequestID)
	pending.TraceID = strings.TrimSpace(command.TraceID)
	pending.ServiceName = serviceNameOrDefault(command.ServiceName)
	pending.ServiceInstanceID = strings.TrimSpace(command.ServiceInstanceID)
	pending.ServiceInstanceIP = strings.TrimSpace(command.ServiceInstanceIP)
	pending.Payload = payload
	pending.SyncStatus = models.PendingStatusAccepted
	if err := models.RunLocalTransaction(func(action models.DataAction) error {
		return pending.InsertWith(action)
	}); err != nil {
		return 0, err
	}
	return command.OrderID, nil
}

func orderFromCommand(command CreateOrderCommand) *models.Order {
	order := models.NewOrder()
	order.ID = command.OrderID
	order.UserID = command.UserID
	order.RequestID = strings.TrimSpace(command.RequestID)
	order.RequestFingerprint = strings.TrimSpace(command.RequestFingerprint)
	order.SupplierID = command.SupplierID
	order.ProductID = command.ProductID
	order.SupplierCode = command.SupplierCode
	order.SupplierName = command.SupplierName
	order.ProductCode = command.ProductCode
	order.ProductName = command.ProductName
	order.UnitPrice = command.UnitPrice
	order.Quantity = command.Quantity
	order.TotalAmount = command.UnitPrice.Mul(modelsDecimalFromInt(command.Quantity))
	order.Recipient = command.Recipient
	order.Phone = command.Phone
	order.Region = command.Region
	order.AddressDetail = command.AddressDetail
	order.AddressID = command.AddressID
	order.TraceID = strings.TrimSpace(command.TraceID)
	order.ServiceName = serviceNameOrDefault(command.ServiceName)
	order.ServiceInstanceID = strings.TrimSpace(command.ServiceInstanceID)
	order.ServiceInstanceIP = strings.TrimSpace(command.ServiceInstanceIP)
	return order
}
