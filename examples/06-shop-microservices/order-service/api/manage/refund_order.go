package manage

import (
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
	result, err, _ := own.GetInstance().(*OrderManage).DoBefore(own, req)
	return result, err
}
func (own *RefundOrder) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
