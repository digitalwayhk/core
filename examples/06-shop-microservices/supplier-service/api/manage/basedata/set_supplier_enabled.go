package basedata

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

// SetSupplierEnabled 是供应商启停的唯一写入口。
type SetSupplierEnabled struct {
	managepkg.Operation[models.Supplier]
}

func NewSetSupplierEnabled(owner interface{}) *SetSupplierEnabled {
	return &SetSupplierEnabled{Operation: managepkg.NewOperation[models.Supplier](owner)}
}

func (own *SetSupplierEnabled) New(instance interface{}) servertypes.IRouter {
	next := NewSetSupplierEnabled(nil)
	next.Operation.New(instance)
	return next
}

func (own *SetSupplierEnabled) Validation(servertypes.IRequest) error {
	if own.Model == nil || own.Model.ID == 0 {
		return contract.ErrResourceNotFound
	}
	return nil
}

func (own *SetSupplierEnabled) Do(req servertypes.IRequest) (interface{}, error) {
	owner, ok := own.GetInstance().(*SupplierManage)
	if !ok {
		return nil, contract.ErrForbidden
	}
	result, err, stop := owner.DoBefore(own, req)
	if stop || err != nil || result != nil {
		return result, err
	}
	return business.SetSupplierEnabled(own.Model.ID, own.Model.Enabled, req.GetTraceId(), models.EventID(req.NewID()))
}

func (own *SetSupplierEnabled) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
