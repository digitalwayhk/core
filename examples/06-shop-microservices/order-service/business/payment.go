package business

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
)

func EnabledPaymentTypes() ([]*orderdto.PaymentType, error) {
	items, err := models.ListPaymentTypes(true)
	if err != nil {
		return nil, err
	}
	result := make([]*orderdto.PaymentType, 0, len(items))
	for _, item := range items {
		result = append(result, &orderdto.PaymentType{ID: item.ID, Name: item.Name, Code: item.Code, Enabled: item.Enabled})
	}
	return result, nil
}

func CreatePayment(userID string, orderID, paymentTypeID, paymentID uint, eventID string) (*orderdto.PaymentRecord, error) {
	order, err := models.FindOrder(orderID)
	if err != nil || order == nil || order.UserID != strings.TrimSpace(userID) {
		return nil, contract.ErrResourceNotFound
	}
	if order.PaymentStatus != models.PaymentStatusUnpaid {
		return nil, errors.New("订单不是未支付状态")
	}
	paymentType, err := models.FindPaymentType(paymentTypeID)
	if err != nil || paymentType == nil || !paymentType.Enabled {
		return nil, errors.New("支付类型不存在或已禁用")
	}
	record := models.NewPaymentRecord()
	record.SetID(paymentID)
	record.OrderID = order.ID
	record.PaymentTypeID = paymentType.ID
	record.Amount = order.UnitPrice.Mul(decimal.NewFromInt(int64(order.Quantity)))
	record.Status = models.PaymentStatusProcessing
	order.PaymentStatus = models.PaymentStatusProcessing
	order.PaymentID = record.ID
	outbox, err := models.NewOutboxRecord(eventID, contract.EventPaymentChanged, contract.SubjectPaymentChanged, models.ChangeEvent(eventID, contract.EventPaymentChanged, "payment_processing", order))
	if err != nil {
		return nil, err
	}
	err = models.RunTransaction(func(a persistencetypes.IDataAction) error {
		if err := record.InsertWith(a); err != nil {
			return err
		}
		if err := order.UpdateWith(a); err != nil {
			return err
		}
		return a.Insert(outbox)
	})
	if err != nil {
		return nil, err
	}
	return &orderdto.PaymentRecord{ID: record.ID, OrderID: record.OrderID, PaymentTypeID: record.PaymentTypeID, Amount: record.Amount, Status: record.Status}, nil
}

func ConfirmPayment(paymentID uint, eventID string) (*orderdto.Order, error) {
	record, err := models.FindPaymentRecord(paymentID)
	if err != nil || record == nil {
		return nil, errors.New("支付流水不存在")
	}
	order, err := models.FindOrder(record.OrderID)
	if err != nil || order == nil {
		return nil, errors.New("订单不存在")
	}
	record.Status = models.PaymentStatusPaid
	order.PaymentStatus = models.PaymentStatusPaid
	outbox, err := models.NewOutboxRecord(eventID, contract.EventPaymentChanged, contract.SubjectPaymentChanged, models.ChangeEvent(eventID, contract.EventPaymentChanged, "paid", order))
	if err != nil {
		return nil, err
	}
	err = models.RunTransaction(func(a persistencetypes.IDataAction) error {
		if err := record.UpdateWith(a); err != nil {
			return err
		}
		if err := order.UpdateWith(a); err != nil {
			return err
		}
		return a.Insert(outbox)
	})
	return models.ToDTO(order), err
}
