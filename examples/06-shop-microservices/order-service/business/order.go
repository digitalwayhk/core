// Package business 实现订单幂等、所有权和状态规则。
package business

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

func CreateOrder(id uint, userID, idempotencyKey, eventID string, product supplierdto.ProductSnapshot, address userdto.AddressSnapshot, quantity int) (*orderdto.Order, error) {
	userID, idempotencyKey = strings.TrimSpace(userID), strings.TrimSpace(idempotencyKey)
	if userID == "" || idempotencyKey == "" || product.ProductID == 0 || product.SupplierID == "" || address.AddressID == 0 || quantity <= 0 {
		return nil, errors.New("下单参数不完整")
	}
	if existing, _ := models.FindByIdempotency(idempotencyKey); existing != nil {
		if existing.UserID != userID {
			return nil, contract.ErrForbidden
		}
		return models.ToDTO(existing), nil
	}
	item := models.NewOrder()
	item.SetID(id)
	item.IdempotencyKey = idempotencyKey
	item.UserID = userID
	item.SupplierID = product.SupplierID
	item.ProductID = product.ProductID
	item.SupplierName = product.SupplierName
	item.ProductCode = product.ProductCode
	item.ProductName = product.ProductName
	item.UnitPrice = product.UnitPrice
	item.Quantity = quantity
	item.AddressID = address.AddressID
	item.Recipient = address.Recipient
	item.Phone = address.Phone
	item.Region = address.Region
	item.AddressDetail = address.Detail
	outbox, err := models.NewOutboxRecord(eventID, contract.EventOrderChanged, contract.SubjectOrderChanged, models.ChangeEvent(eventID, "created", item))
	if err != nil {
		return nil, err
	}
	err = models.RunTransaction(func(a persistencetypes.IDataAction) error {
		if err := item.InsertWith(a); err != nil {
			return err
		}
		return a.Insert(outbox)
	})
	return models.ToDTO(item), err
}

func UserOrders(userID string) ([]*orderdto.Order, error) {
	items, err := models.ListOrders("UserID", strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	result := make([]*orderdto.Order, 0, len(items))
	for _, item := range items {
		result = append(result, models.ToDTO(item))
	}
	return result, nil
}
func SupplierOrders(supplierID string) ([]*orderdto.SupplierOrder, error) {
	items, err := models.ListOrders("SupplierID", strings.TrimSpace(supplierID))
	if err != nil {
		return nil, err
	}
	result := make([]*orderdto.SupplierOrder, 0, len(items))
	for _, item := range items {
		dto := models.ToDTO(item)
		result = append(result, &orderdto.SupplierOrder{ID: dto.ID, ProductID: dto.Product.ProductID, ProductName: dto.Product.ProductName, Quantity: dto.Quantity, TotalAmount: dto.TotalAmount, PaymentStatus: dto.PaymentStatus, Status: dto.Status, CreatedAt: dto.CreatedAt})
	}
	return result, nil
}
func DeleteOrCancel(userID string, id uint, eventID string) (*orderdto.Order, error) {
	item, err := models.FindOrder(id)
	if err != nil || item == nil || item.UserID != strings.TrimSpace(userID) {
		return nil, contract.ErrResourceNotFound
	}
	action := "deleted"
	deleteOrder := item.PaymentStatus == models.PaymentStatusUnpaid
	if !deleteOrder {
		item.Status = models.OrderStatusCancelled
		action = "cancelled"
	}
	outbox, err := models.NewOutboxRecord(eventID, contract.EventOrderChanged, contract.SubjectOrderChanged, models.ChangeEvent(eventID, action, item))
	if err != nil {
		return nil, err
	}
	err = models.RunTransaction(func(a persistencetypes.IDataAction) error {
		if deleteOrder {
			if err := item.DeleteWith(a); err != nil {
				return err
			}
		} else if err := item.UpdateWith(a); err != nil {
			return err
		}
		return a.Insert(outbox)
	})
	if err != nil {
		return nil, err
	}
	return models.ToDTO(item), nil
}
