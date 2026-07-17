package business

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
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

func CreatePayment(userID, orderID, paymentTypeID uint, paymentID, eventID string) (*orderdto.PaymentRecord, error) {
	paymentID, eventID = strings.TrimSpace(paymentID), strings.TrimSpace(eventID)
	if userID == 0 || orderID == 0 || paymentTypeID == 0 || paymentID == "" || eventID == "" {
		return nil, errors.New("支付参数不完整")
	}
	var result *models.PaymentRecord
	err := models.RunTransaction(func(action persistencetypes.IDataAction) error {
		order, err := models.FindOrderWith(action, orderID)
		if err != nil || order == nil || order.UserID != userID {
			return contract.ErrResourceNotFound
		}
		if order.OrderStatus != models.OrderStatusNormal || (order.PaymentStatus != models.PaymentStatusUnpaid && order.PaymentStatus != models.PaymentStatusFailed) {
			return errors.New("订单当前状态不允许支付")
		}
		paymentType, err := models.FindPaymentTypeWith(action, paymentTypeID)
		if err != nil || paymentType == nil || !paymentType.Enabled {
			return errors.New("支付类型不存在或已禁用")
		}
		records, err := models.ListPaymentRecordsWith(action, order.ID)
		if err != nil {
			return err
		}
		attempt := uint(1)
		for _, record := range records {
			if record.Status == models.PaymentStatusProcessing {
				return errors.New("订单已有处理中支付")
			}
			if record.Attempt >= attempt {
				attempt = record.Attempt + 1
			}
		}
		record := models.NewPaymentRecord()
		record.OrderID, record.PaymentTypeID, record.Attempt, record.PaymentID = order.ID, paymentType.ID, attempt, paymentID
		record.Amount, record.Status = order.TotalAmount, models.PaymentStatusProcessing
		if err := record.InsertWith(action); err != nil {
			return err
		}
		order.PaymentStatus, order.CurrentPaymentID = models.PaymentStatusProcessing, paymentID
		order.OrderRevision++
		if err := order.UpdateWith(action); err != nil {
			return err
		}
		outbox, err := models.NewOutboxRecord(eventID, contract.EventPaymentChanged, contract.SubjectPaymentChanged, models.ChangeEvent(eventID, contract.EventPaymentChanged, "payment_processing", order))
		if err != nil {
			return err
		}
		if err := action.Insert(outbox); err != nil {
			return err
		}
		result = record
		return nil
	})
	return models.PaymentToDTO(result), err
}

func changePayment(paymentID, eventID string, from, targetPayment, targetOrder int, actionName string) (*orderdto.Order, error) {
	var result *models.Order
	err := models.RunTransaction(func(action persistencetypes.IDataAction) error {
		record, err := models.FindPaymentByPaymentIDWith(action, paymentID)
		if err != nil || record == nil {
			return contract.ErrResourceNotFound
		}
		order, err := models.FindOrderWith(action, record.OrderID)
		if err != nil || order == nil || order.CurrentPaymentID != record.PaymentID {
			return contract.ErrResourceNotFound
		}
		if record.Status == targetPayment && order.PaymentStatus == targetPayment {
			result = order
			return nil
		}
		if record.Status != from || order.PaymentStatus != from {
			return errors.New("支付状态转换无效")
		}
		record.Status = targetPayment
		if err := record.UpdateWith(action); err != nil {
			return err
		}
		order.PaymentStatus = targetPayment
		if targetOrder >= 0 {
			order.OrderStatus = targetOrder
		}
		order.OrderRevision++
		if err := order.UpdateWith(action); err != nil {
			return err
		}
		outbox, err := models.NewOutboxRecord(eventID, contract.EventPaymentChanged, contract.SubjectPaymentChanged, models.ChangeEvent(eventID, contract.EventPaymentChanged, actionName, order))
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

func ConfirmPayment(paymentID, eventID string) (*orderdto.Order, error) {
	return changePayment(paymentID, eventID, models.PaymentStatusProcessing, models.PaymentStatusPaid, -1, "paid")
}

func FailPayment(paymentID, eventID string) (*orderdto.Order, error) {
	return changePayment(paymentID, eventID, models.PaymentStatusProcessing, models.PaymentStatusFailed, -1, "payment_failed")
}

func ConfirmRefund(paymentID, eventID string) (*orderdto.Order, error) {
	return changePayment(paymentID, eventID, models.PaymentStatusRefunding, models.PaymentStatusRefunded, models.OrderStatusCancelled, "refunded")
}
