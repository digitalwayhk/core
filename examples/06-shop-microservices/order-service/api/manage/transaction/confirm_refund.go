package transaction

import (
	"strconv"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
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
	owner, ok := own.GetInstance().(*PaymentRecordManage)
	if !ok {
		return nil, contract.ErrForbidden
	}
	result, err, stop := owner.DoBefore(own, req)
	if stop || err != nil || result != nil {
		return result, err
	}
	return handlePaymentCommand(own.Model, req.GetTraceId(), strconv.FormatUint(uint64(req.NewID()), 10), business.ConfirmRefund)
}
func (own *ConfirmRefund) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
