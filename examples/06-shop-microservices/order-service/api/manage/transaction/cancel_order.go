package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

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
