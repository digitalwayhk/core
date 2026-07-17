package manage

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

type SetPaymentTypeEnabled struct {
	managepkg.Operation[models.PaymentType]
}

func NewSetPaymentTypeEnabled(owner interface{}) *SetPaymentTypeEnabled {
	return &SetPaymentTypeEnabled{Operation: managepkg.NewOperation[models.PaymentType](owner)}
}
func (own *SetPaymentTypeEnabled) New(instance interface{}) servertypes.IRouter {
	next := NewSetPaymentTypeEnabled(nil)
	next.Operation.New(instance)
	return next
}
func (own *SetPaymentTypeEnabled) Validation(servertypes.IRequest) error {
	if own.Model == nil || own.Model.ID == 0 {
		return contract.ErrResourceNotFound
	}
	return nil
}
func (own *SetPaymentTypeEnabled) Do(req servertypes.IRequest) (interface{}, error) {
	result, err, _ := own.GetInstance().(*PaymentTypeManage).DoBefore(own, req)
	return result, err
}
func (own *SetPaymentTypeEnabled) RouterInfo() *servertypes.RouterInfo {
	return managepkg.RouterInfo(own)
}

type CancelOrder struct {
	managepkg.Operation[models.Order]
}

func NewCancelOrder(owner interface{}) *CancelOrder {
	return &CancelOrder{Operation: managepkg.NewOperation[models.Order](owner)}
}
func (own *CancelOrder) New(instance interface{}) servertypes.IRouter {
	next := NewCancelOrder(nil)
	next.Operation.New(instance)
	return next
}
func (own *CancelOrder) Do(req servertypes.IRequest) (interface{}, error) {
	result, err, _ := own.GetInstance().(*OrderManage).DoBefore(own, req)
	return result, err
}
func (own *CancelOrder) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }

type RefundOrder struct {
	managepkg.Operation[models.Order]
}

func NewRefundOrder(owner interface{}) *RefundOrder {
	return &RefundOrder{Operation: managepkg.NewOperation[models.Order](owner)}
}
func (own *RefundOrder) New(instance interface{}) servertypes.IRouter {
	next := NewRefundOrder(nil)
	next.Operation.New(instance)
	return next
}
func (own *RefundOrder) Do(req servertypes.IRequest) (interface{}, error) {
	result, err, _ := own.GetInstance().(*OrderManage).DoBefore(own, req)
	return result, err
}
func (own *RefundOrder) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }

type ConfirmPayment struct {
	managepkg.Operation[models.PaymentRecord]
}

func NewConfirmPayment(owner interface{}) *ConfirmPayment {
	return &ConfirmPayment{Operation: managepkg.NewOperation[models.PaymentRecord](owner)}
}
func (own *ConfirmPayment) New(instance interface{}) servertypes.IRouter {
	next := NewConfirmPayment(nil)
	next.Operation.New(instance)
	return next
}
func (own *ConfirmPayment) Do(req servertypes.IRequest) (interface{}, error) {
	result, err, _ := own.GetInstance().(*PaymentRecordManage).DoBefore(own, req)
	return result, err
}
func (own *ConfirmPayment) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }

type FailPayment struct {
	managepkg.Operation[models.PaymentRecord]
}

func NewFailPayment(owner interface{}) *FailPayment {
	return &FailPayment{Operation: managepkg.NewOperation[models.PaymentRecord](owner)}
}
func (own *FailPayment) New(instance interface{}) servertypes.IRouter {
	next := NewFailPayment(nil)
	next.Operation.New(instance)
	return next
}
func (own *FailPayment) Do(req servertypes.IRequest) (interface{}, error) {
	result, err, _ := own.GetInstance().(*PaymentRecordManage).DoBefore(own, req)
	return result, err
}
func (own *FailPayment) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }

type ConfirmRefund struct {
	managepkg.Operation[models.PaymentRecord]
}

func NewConfirmRefund(owner interface{}) *ConfirmRefund {
	return &ConfirmRefund{Operation: managepkg.NewOperation[models.PaymentRecord](owner)}
}
func (own *ConfirmRefund) New(instance interface{}) servertypes.IRouter {
	next := NewConfirmRefund(nil)
	next.Operation.New(instance)
	return next
}
func (own *ConfirmRefund) Do(req servertypes.IRequest) (interface{}, error) {
	result, err, _ := own.GetInstance().(*PaymentRecordManage).DoBefore(own, req)
	return result, err
}
func (own *ConfirmRefund) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
