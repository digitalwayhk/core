package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

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
