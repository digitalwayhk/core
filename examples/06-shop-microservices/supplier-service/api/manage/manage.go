package manage

import (
	"errors"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
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
func (s *SupplierManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{s.View, s.Search, s.Edit}
}
func (*SupplierManage) DoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	operation, ok := sender.(*managepkg.Edit[models.Supplier])
	if !ok {
		return nil, nil, false
	}
	if operation.Model == nil || operation.OldItem == nil {
		return nil, errors.New("供应商编辑参数无效"), true
	}
	updated, err := business.UpdateSupplier((*operation.OldItem).ID, (*operation.Model).Name, (*operation.Model).Enabled, models.EventID(req.NewID()))
	return updated, err, true
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
