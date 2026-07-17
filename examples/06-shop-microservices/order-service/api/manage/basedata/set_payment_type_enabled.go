package basedata

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
