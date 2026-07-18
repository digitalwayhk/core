package transaction

import (
	"strconv"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
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
	owner, ok := own.GetInstance().(*PaymentRecordManage)
	if !ok {
		return nil, contract.ErrForbidden
	}
	result, err, stop := owner.DoBefore(own, req)
	if stop || err != nil || result != nil {
		return result, err
	}
	return handlePaymentCommand(own.Model, strconv.FormatUint(uint64(req.NewID()), 10), business.ConfirmPayment)
}
func (own *ConfirmPayment) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
