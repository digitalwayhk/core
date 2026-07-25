// 本文件提供当前服务业务层的事务编排、状态变更和事件写入能力。
package business

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

func paymentTypeEvent(traceID, eventID, action string, item *models.PaymentType) eventdto.PaymentTypeChanged {
	return eventdto.PaymentTypeChanged{Metadata: eventdto.Metadata{
		EventID: eventID, TraceID: traceID, SchemaVersion: contract.EventSchemaVersion, EventType: contract.EventPaymentTypeChanged,
		OccurredAt: time.Now().UTC(), SourceService: contract.OrderServiceName, AggregateID: strconv.FormatUint(uint64(item.ID), 10),
	}, PaymentTypeID: item.ID, Code: item.Code, Name: item.Name, Enabled: item.Enabled, Action: action}
}

func writePaymentTypeEvent(action persistencetypes.IDataAction, traceID, eventID, actionName string, item *models.PaymentType) error {
	outbox, err := models.NewOutboxRecord(traceID, eventID, contract.EventPaymentTypeChanged, contract.SubjectPaymentTypeChanged, paymentTypeEvent(traceID, eventID, actionName, item))
	if err != nil {
		return err
	}
	return action.Insert(outbox)
}

// CreatePaymentType 执行本文件能力对应的业务操作。
func CreatePaymentType(input *models.PaymentType, traceID, eventID string) (*models.PaymentType, error) {
	item := models.NewPaymentType()
	item.TraceID = strings.TrimSpace(traceID)
	item.SetID(input.ID)
	item.Name, item.Code, item.Enabled = input.Name, input.Code, false
	err := models.RunTransaction(func(action persistencetypes.IDataAction) error {
		if err := item.InsertWith(action); err != nil {
			return err
		}
		return writePaymentTypeEvent(action, traceID, eventID, "created", item)
	})
	return item, err
}

// UpdatePaymentType 执行本文件能力对应的业务操作。
func UpdatePaymentType(id uint, name, code, traceID, eventID string) (*models.PaymentType, error) {
	traceID = strings.TrimSpace(traceID)
	var result *models.PaymentType
	err := models.RunTransaction(func(action persistencetypes.IDataAction) error {
		item, err := models.FindPaymentTypeWith(action, id)
		if err != nil || item == nil {
			return contract.ErrResourceNotFound
		}
		code = strings.ToLower(strings.TrimSpace(code))
		if code != item.Code {
			used, useErr := models.PaymentTypeInUseWith(action, id)
			if useErr != nil {
				return useErr
			}
			if used {
				return contract.ErrResourceInUse
			}
		}
		item.Name, item.Code = name, code
		item.TraceID = traceID
		if err := item.UpdateWith(action); err != nil {
			return err
		}
		if err := writePaymentTypeEvent(action, traceID, eventID, "updated", item); err != nil {
			return err
		}
		result = item
		return nil
	})
	return result, err
}

// SetPaymentTypeEnabled 执行本文件能力对应的业务操作。
func SetPaymentTypeEnabled(id uint, enabled bool, traceID, eventID string) (*models.PaymentType, error) {
	traceID = strings.TrimSpace(traceID)
	var result *models.PaymentType
	err := models.RunTransaction(func(action persistencetypes.IDataAction) error {
		item, err := models.FindPaymentTypeWith(action, id)
		if err != nil || item == nil {
			return contract.ErrResourceNotFound
		}
		item.Enabled = enabled
		item.TraceID = traceID
		if err := item.UpdateWith(action); err != nil {
			return err
		}
		if err := writePaymentTypeEvent(action, traceID, eventID, "enabled_changed", item); err != nil {
			return err
		}
		result = item
		return nil
	})
	return result, err
}

// DeletePaymentType 执行本文件能力对应的业务操作。
func DeletePaymentType(id uint, traceID, eventID string) (*models.PaymentType, error) {
	traceID = strings.TrimSpace(traceID)
	var result *models.PaymentType
	err := models.RunTransaction(func(action persistencetypes.IDataAction) error {
		item, err := models.FindPaymentTypeWith(action, id)
		if err != nil || item == nil {
			return contract.ErrResourceNotFound
		}
		used, err := models.PaymentTypeInUseWith(action, id)
		if err != nil {
			return err
		}
		if used {
			return contract.ErrResourceInUse
		}
		if err := item.DeleteWith(action); err != nil {
			return err
		}
		item.TraceID = traceID
		if err := writePaymentTypeEvent(action, traceID, eventID, "deleted", item); err != nil {
			return err
		}
		result = item
		return nil
	})
	return result, err
}

// EnabledPaymentTypes 执行本文件能力对应的业务操作。
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

// CreatePayment 执行本文件能力对应的业务操作。
func CreatePayment(userID, orderID, paymentTypeID uint, paymentID, traceID, eventID string) (*orderdto.PaymentRecord, error) {
	paymentID, eventID = strings.TrimSpace(paymentID), strings.TrimSpace(eventID)
	traceID = strings.TrimSpace(traceID)
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
		record.TraceID = traceID
		record.OrderID, record.PaymentTypeID, record.Attempt, record.PaymentID = order.ID, paymentType.ID, attempt, paymentID
		record.Amount, record.Status = order.TotalAmount, models.PaymentStatusProcessing
		if err := record.InsertWith(action); err != nil {
			return err
		}
		order.TraceID, order.PaymentStatus, order.CurrentPaymentID = traceID, models.PaymentStatusProcessing, paymentID
		order.OrderRevision++
		if err := order.UpdateWith(action); err != nil {
			return err
		}
		outbox, err := models.NewOutboxRecord(traceID, eventID, contract.EventPaymentChanged, contract.SubjectPaymentChanged, models.ChangeEvent(traceID, eventID, contract.EventPaymentChanged, "payment_processing", order))
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

func changePayment(paymentID, traceID, eventID string, from, targetPayment, targetOrder int, actionName string) (*orderdto.Order, error) {
	traceID = strings.TrimSpace(traceID)
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
		record.TraceID, record.Status = traceID, targetPayment
		if err := record.UpdateWith(action); err != nil {
			return err
		}
		order.TraceID, order.PaymentStatus = traceID, targetPayment
		if targetOrder >= 0 {
			order.OrderStatus = targetOrder
		}
		order.OrderRevision++
		if err := order.UpdateWith(action); err != nil {
			return err
		}
		outbox, err := models.NewOutboxRecord(traceID, eventID, contract.EventPaymentChanged, contract.SubjectPaymentChanged, models.ChangeEvent(traceID, eventID, contract.EventPaymentChanged, actionName, order))
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

// ConfirmPayment 执行本文件能力对应的业务操作。
func ConfirmPayment(paymentID, traceID, eventID string) (*orderdto.Order, error) {
	return changePayment(paymentID, traceID, eventID, models.PaymentStatusProcessing, models.PaymentStatusPaid, -1, "paid")
}

// FailPayment 执行本文件能力对应的业务操作。
func FailPayment(paymentID, traceID, eventID string) (*orderdto.Order, error) {
	return changePayment(paymentID, traceID, eventID, models.PaymentStatusProcessing, models.PaymentStatusFailed, -1, "payment_failed")
}

// ConfirmRefund 执行本文件能力对应的业务操作。
func ConfirmRefund(paymentID, traceID, eventID string) (*orderdto.Order, error) {
	return changePayment(paymentID, traceID, eventID, models.PaymentStatusRefunding, models.PaymentStatusRefunded, models.OrderStatusCancelled, "refunded")
}
