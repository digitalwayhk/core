package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	commonmanage "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/manage/common"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// OrderManage 只提供供应商订单投影的查询能力。
type OrderManage struct {
	*managepkg.ManageService[models.SupplierOrder]
}

func NewOrderManage() *OrderManage {
	own := &OrderManage{}
	own.ManageService = managepkg.NewManageService[models.SupplierOrder](own)
	return own
}

func (own *OrderManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search}
}

func (*OrderManage) SearchBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	search, ok := sender.(*managepkg.Search[models.SupplierOrder])
	if !ok {
		return nil, contract.ErrResourceNotFound, true
	}
	return commonmanage.AddOwnerWhere(search.SearchItem, req, "SupplierID")
}

func (*OrderManage) ViewModel(model *view.ViewModel) {
	model.Title = "供应商订单查询"
	model.AutoLoad = true
}
