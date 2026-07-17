package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

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
