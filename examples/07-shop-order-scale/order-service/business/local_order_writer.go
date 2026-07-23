// Package business 实现 07 订单服务本地可靠写入能力。
package business

import (
	"context"
	"errors"
	"strings"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
)

// LocalOrderWriter 将订单请求先可靠写入当前实例本地 pending。
type LocalOrderWriter struct {
	Store models.OrderWriteAccess
}

// Accept 校验订单命令并在本地 pending 持久成功后返回订单 ID。
func (writer LocalOrderWriter) Accept(ctx context.Context, command CreateOrderCommand) (uint, error) {
	if err := command.validate(); err != nil {
		return 0, err
	}
	if writer.Store == nil {
		return 0, models.ErrOrderWriteStoreUnavailable
	}
	order := orderFromCommand(command)
	existing, err := writer.Store.FindLocalByRequest(ctx, command.UserID, strings.TrimSpace(command.RequestID))
	if err != nil {
		return 0, err
	}
	if existing != nil {
		if existing.RequestFingerprint != strings.TrimSpace(command.RequestFingerprint) {
			return 0, errors.New("幂等键已用于不同订单请求")
		}
		return existing.ID, nil
	}
	if command.EnableRemoteIdempotency {
		existing, err := findRemoteOrderByRequest(command.UserID, strings.TrimSpace(command.RequestID))
		if err != nil {
			return 0, err
		}
		if existing != nil {
			if existing.RequestFingerprint != strings.TrimSpace(command.RequestFingerprint) {
				return 0, errors.New("幂等键已用于不同订单请求")
			}
			return existing.ID, nil
		}
	}
	if err := writer.Store.Save(ctx, order); err != nil {
		return 0, err
	}
	return command.OrderID, nil
}

func findRemoteOrderByRequest(userID uint, requestID string) (*models.Order, error) {
	var order *models.Order
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		found, err := models.FindRemoteOrderByIdempotencyWith(action, userID, requestID)
		if err != nil {
			return nil
		}
		order = found
		return nil
	})
	return order, err
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
