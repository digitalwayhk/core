package basedata

import (
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/business"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	"github.com/digitalwayhk/core/service/manage/view"
)

// SupplierManage 继承基础资料能力，并将商品集合限制为只读。
type SupplierManage struct {
	*BaseDataManage[models.Supplier]
}

// NewSupplierManage 创建以具体供应商 Manage 为最终 owner 的管理服务。
func NewSupplierManage() *SupplierManage {
	own := &SupplierManage{}
	own.BaseDataManage = NewBaseDataManage[models.Supplier](own)
	return own
}

// ViewModel 设置供应商管理页面。
func (own *SupplierManage) ViewModel(model *view.ViewModel) {
	model.Title = "供应商管理"
	model.AutoLoad = true
}

// ViewFieldModel 应用基础资料公共字段规则。
func (own *SupplierManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.BaseDataManage.ViewFieldModel(model, field)
}

// ViewChildModel 将供应商商品集合配置为只读。
func (own *SupplierManage) ViewChildModel(child *view.ViewChildModel) {
	own.BaseDataManage.ViewChildModel(child)
	if child.Name == "Products" {
		child.Title = "供应商商品"
		child.IsAdd = false
		child.IsEdit = false
		child.IsRemove = false
	}
}

// SetBaseDataEnabled 供继承的通用启停命令调用。
func (own *SupplierManage) SetBaseDataEnabled(id uint, enabled bool) (*models.Supplier, error) {
	return business.NewSupplierService().SetEnabled(id, enabled)
}
