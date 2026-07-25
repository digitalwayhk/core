package basedata

import (
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/business"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ProductManage 继承基础资料 CRUD、启停能力并追加商品规则。
type ProductManage struct {
	*BaseDataManage[models.Product]
}

// NewProductManage 创建以具体商品 Manage 为最终 owner 的管理服务。
func NewProductManage() *ProductManage {
	own := &ProductManage{}
	own.BaseDataManage = NewBaseDataManage[models.Product](own)
	return own
}

// ViewModel 设置商品管理页面。
func (own *ProductManage) ViewModel(model *view.ViewModel) {
	model.Title = "商品管理"
	model.AutoLoad = true
}

// ViewFieldModel 先应用基础资料规则，再配置商品字段。
func (own *ProductManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.BaseDataManage.ViewFieldModel(model, field)
	if field.IsFieldOrTitle("Price") {
		field.Title = "商品价格"
		field.Precision = 2
	}
	if field.IsFieldOrTitle("SupplierID") {
		field.Title = "供应商"
		field.IsSearch = true
	}
}

// OnAddBefore 先执行基础资料的默认禁用和唯一性校验，再追加“供应商必须存在且已启用”规则。
func (own *ProductManage) OnAddBefore(operation *managepkg.Add[models.Product], req servertypes.IRequest) (interface{}, error, bool) {
	data, err, stop := own.BaseDataManage.OnAddBefore(operation, req)
	if stop || err != nil {
		return data, err, stop
	}
	if operation != nil && operation.Model != nil {
		if err := business.NewProductService().EnsureSupplierEnabled(operation.Model.SupplierID); err != nil {
			return nil, err, true
		}
	}
	return nil, nil, false
}

// OnEditBefore 先执行基础资料的状态保护和更新校验，再检查新供应商可用性。
func (own *ProductManage) OnEditBefore(operation *managepkg.Edit[models.Product], req servertypes.IRequest) (interface{}, error, bool) {
	data, err, stop := own.BaseDataManage.OnEditBefore(operation, req)
	if stop || err != nil {
		return data, err, stop
	}
	if operation != nil && operation.Model != nil {
		if err := business.NewProductService().EnsureSupplierEnabled(operation.Model.SupplierID); err != nil {
			return nil, err, true
		}
	}
	return nil, nil, false
}

// SetBaseDataEnabled 供继承的通用启停命令调用。
func (own *ProductManage) SetBaseDataEnabled(id uint, enabled bool) (*models.Product, error) {
	return business.NewProductService().SetEnabled(id, enabled)
}
