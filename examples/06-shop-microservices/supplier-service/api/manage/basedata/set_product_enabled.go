package basedata

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

// SetProductEnabled 是商品上下架的唯一写入口。
type SetProductEnabled struct {
	managepkg.Operation[models.Product]
}

func NewSetProductEnabled(owner interface{}) *SetProductEnabled {
	return &SetProductEnabled{Operation: managepkg.NewOperation[models.Product](owner)}
}

func (own *SetProductEnabled) New(instance interface{}) servertypes.IRouter {
	next := NewSetProductEnabled(nil)
	next.Operation.New(instance)
	return next
}

func (own *SetProductEnabled) Validation(servertypes.IRequest) error {
	if own.Model == nil || own.Model.ID == 0 {
		return contract.ErrResourceNotFound
	}
	return nil
}

func (own *SetProductEnabled) Do(req servertypes.IRequest) (interface{}, error) {
	owner, ok := own.GetInstance().(*ProductManage)
	if !ok {
		return nil, contract.ErrForbidden
	}
	result, err, stop := owner.DoBefore(own, req)
	if stop || err != nil || result != nil {
		return result, err
	}
	return business.SetProductEnabled(own.Model.ID, own.Model.Enabled, req.GetTraceId(), models.EventID(req.NewID()))
}

func (own *SetProductEnabled) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
