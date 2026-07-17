package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

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
