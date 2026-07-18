package transaction

import (
	"strconv"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

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
	owner, ok := own.GetInstance().(*OrderManage)
	if !ok {
		return nil, contract.ErrForbidden
	}
	result, err, stop := owner.DoBefore(own, req)
	if stop || err != nil || result != nil {
		return result, err
	}
	return cancelSelectedOrder(own.Model, req.GetTraceId(), strconv.FormatUint(uint64(req.NewID()), 10))
}
func (own *RefundOrder) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
