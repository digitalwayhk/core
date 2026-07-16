package manage

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// SupplierManage 供平台管理员查看、启停和维护供应商。
type SupplierManage struct {
	*managepkg.ManageService[models.Supplier]
}

func NewSupplierManage() *SupplierManage {
	own := &SupplierManage{}
	own.ManageService = managepkg.NewManageService[models.Supplier](own)
	return own
}
func (*SupplierManage) ViewModel(model *view.ViewModel) {
	model.Title = "供应商管理"
	model.AutoLoad = true
}

// ProductManage 供平台管理员查看全部供应商商品。
type ProductManage struct {
	*managepkg.ManageService[models.Product]
}

func NewProductManage() *ProductManage {
	own := &ProductManage{}
	own.ManageService = managepkg.NewManageService[models.Product](own)
	return own
}
func (p *ProductManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{p.View, p.Search}
}
func (*ProductManage) ViewModel(model *view.ViewModel) {
	model.Title = "商品查询"
	model.AutoLoad = true
}
