// Package business 实现订单幂等、所有权和状态规则。
package business

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// CreateOrderCommand 定义本文件能力使用的核心结构。
type CreateOrderCommand struct {
	OrderID   uint
	UserID    uint
	RequestID string
	TraceID   string
	EventID   string
	ProductID uint
	Quantity  int
	Address   userdto.AddressSnapshot
}

func normalizeAddress(address userdto.AddressSnapshot) userdto.AddressSnapshot {
	address.Recipient = strings.TrimSpace(address.Recipient)
	address.Phone = strings.TrimSpace(address.Phone)
	address.Region = strings.TrimSpace(address.Region)
	address.Detail = strings.TrimSpace(address.Detail)
	return address
}

func requestFingerprint(productID uint, quantity int, address userdto.AddressSnapshot) (string, error) {
	payload := struct {
		ProductID uint                    `json:"productID"`
		Quantity  int                     `json:"quantity"`
		Address   userdto.AddressSnapshot `json:"address"`
	}{ProductID: productID, Quantity: quantity, Address: normalizeAddress(address)}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return utils.HashCodes(string(data)), nil
}

func sameIdempotentRequest(existing *models.Order, command CreateOrderCommand, fingerprint string) bool {
	return existing != nil && existing.UserID == command.UserID && existing.RequestFingerprint == fingerprint
}

func existingOrder(command CreateOrderCommand, fingerprint string) (*orderdto.Order, error) {
	existing, err := models.FindByIdempotency(command.RequestID)
	if err != nil || existing == nil {
		return nil, err
	}
	if !sameIdempotentRequest(existing, command, fingerprint) {
		return nil, contract.ErrIdempotencyKeyReused
	}
	return models.ToDTO(existing), nil
}

// CreateOrder 执行本文件能力对应的业务操作。
func CreateOrder(command CreateOrderCommand, product supplierdto.ProductSnapshot) (*orderdto.Order, error) {
	command.RequestID = strings.TrimSpace(command.RequestID)
	command.TraceID = strings.TrimSpace(command.TraceID)
	command.EventID = strings.TrimSpace(command.EventID)
	command.Address = normalizeAddress(command.Address)
	if command.OrderID == 0 || command.UserID == 0 || command.RequestID == "" || command.EventID == "" || command.ProductID == 0 || command.Quantity <= 0 || command.Address.AddressID == 0 ||
		product.ProductID != command.ProductID || product.SupplierID == 0 || !product.UnitPrice.GreaterThan(decimal.Zero) {
		return nil, errors.New("下单参数不完整")
	}
	fingerprint, err := requestFingerprint(command.ProductID, command.Quantity, command.Address)
	if err != nil {
		return nil, err
	}
	if existing, findErr := existingOrder(command, fingerprint); existing != nil || findErr != nil {
		return existing, findErr
	}

	var result *models.Order
	err = models.RunTransaction(func(action persistencetypes.IDataAction) error {
		if existing, findErr := models.FindByIdempotencyWith(action, command.RequestID); findErr != nil {
			return findErr
		} else if existing != nil {
			if !sameIdempotentRequest(existing, command, fingerprint) {
				return contract.ErrIdempotencyKeyReused
			}
			result = existing
			return nil
		}
		item := models.NewOrder()
		item.SetID(command.OrderID)
		item.TraceID = command.TraceID
		item.IdempotencyKey, item.RequestFingerprint, item.OrderRevision = command.RequestID, fingerprint, 1
		item.UserID, item.SupplierID, item.ProductID = command.UserID, product.SupplierID, product.ProductID
		item.SupplierCode, item.SupplierName = strings.TrimSpace(product.SupplierCode), strings.TrimSpace(product.SupplierName)
		item.ProductCode, item.ProductName, item.UnitPrice = strings.TrimSpace(product.ProductCode), strings.TrimSpace(product.ProductName), product.UnitPrice
		item.Quantity, item.TotalAmount = command.Quantity, product.UnitPrice.Mul(decimal.NewFromInt(int64(command.Quantity)))
		item.AddressID, item.Recipient, item.Phone = command.Address.AddressID, command.Address.Recipient, command.Address.Phone
		item.Region, item.AddressDetail = command.Address.Region, command.Address.Detail
		if err := item.InsertWith(action); err != nil {
			return err
		}
		outbox, outboxErr := models.NewOutboxRecord(command.TraceID, command.EventID, contract.EventOrderCreated, contract.SubjectOrderCreated, models.ChangeEvent(command.TraceID, command.EventID, contract.EventOrderCreated, "created", item))
		if outboxErr != nil {
			return outboxErr
		}
		if err := action.Insert(outbox); err != nil {
			return err
		}
		result = item
		return nil
	})
	if err != nil {
		if existing, findErr := existingOrder(command, fingerprint); existing != nil || findErr != nil {
			return existing, findErr
		}
		return nil, err
	}
	return models.ToDTO(result), nil
}

// UserOrders 执行本文件能力对应的业务操作。
func UserOrders(userID uint) ([]*orderdto.Order, error) {
	items, err := models.ListOrders("UserID", userID)
	if err != nil {
		return nil, err
	}
	result := make([]*orderdto.Order, 0, len(items))
	for _, item := range items {
		result = append(result, models.ToDTO(item))
	}
	return result, nil
}

// CancelOrder 执行本文件能力对应的业务操作。
func CancelOrder(userID, orderID uint, traceID, eventID string) (*orderdto.Order, error) {
	traceID = strings.TrimSpace(traceID)
	var result *models.Order
	err := models.RunTransaction(func(action persistencetypes.IDataAction) error {
		order, err := models.FindOrderWith(action, orderID)
		if err != nil || order == nil || order.UserID != userID {
			return contract.ErrResourceNotFound
		}
		if order.OrderStatus == models.OrderStatusCancelled || order.OrderStatus == models.OrderStatusCancelling {
			result = order
			return nil
		}
		switch order.PaymentStatus {
		case models.PaymentStatusUnpaid, models.PaymentStatusFailed:
			order.OrderStatus = models.OrderStatusCancelled
		case models.PaymentStatusPaid:
			record, findErr := models.FindPaymentByPaymentIDWith(action, order.CurrentPaymentID)
			if findErr != nil || record == nil {
				return errors.New("当前支付流水不存在")
			}
			record.Status = models.PaymentStatusRefunding
			if err := record.UpdateWith(action); err != nil {
				return err
			}
			order.OrderStatus = models.OrderStatusCancelling
			order.PaymentStatus = models.PaymentStatusRefunding
		case models.PaymentStatusRefunding:
			result = order
			return nil
		default:
			return errors.New("当前支付状态不允许撤单")
		}
		order.TraceID = traceID
		order.OrderRevision++
		if err := order.UpdateWith(action); err != nil {
			return err
		}
		outbox, err := models.NewOutboxRecord(traceID, eventID, contract.EventOrderStatusChanged, contract.SubjectOrderStatusChanged, models.ChangeEvent(traceID, eventID, contract.EventOrderStatusChanged, "cancelled", order))
		if err != nil {
			return err
		}
		if err := action.Insert(outbox); err != nil {
			return err
		}
		result = order
		return nil
	})
	return models.ToDTO(result), err
}

// DeleteOrCancel 是旧调用方的迁移别名；订单事实不会再被物理删除。
func DeleteOrCancel(userID uint, orderID uint, eventID string) (*orderdto.Order, error) {
	return CancelOrder(userID, orderID, "", eventID)
}
