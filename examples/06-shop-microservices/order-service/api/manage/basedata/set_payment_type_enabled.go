package basedata

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"strconv"
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
	owner, ok := own.GetInstance().(*PaymentTypeManage)
	if !ok {
		return nil, contract.ErrForbidden
	}
	result, err, stop := owner.DoBefore(own, req)
	if stop || err != nil || result != nil {
		return result, err
	}
	return business.SetPaymentTypeEnabled(own.Model.ID, own.Model.Enabled, strconv.FormatUint(uint64(req.NewID()), 10))
}
func (own *SetPaymentTypeEnabled) RouterInfo() *servertypes.RouterInfo {
	return managepkg.RouterInfo(own)
}
