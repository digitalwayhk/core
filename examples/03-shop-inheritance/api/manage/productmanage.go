package manage

import (
	"github.com/digitalwayhk/core/examples/03-shop-inheritance/business"
	"github.com/digitalwayhk/core/examples/03-shop-inheritance/models"
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

// ValidationAfter 先保留父级校验，再由后续业务实现追加商品规则。
func (own *ProductManage) ValidationAfter(sender interface{}, req servertypes.IRequest) error {
	if err := own.BaseDataManage.ValidationAfter(sender, req); err != nil {
		return err
	}
	service := business.NewProductService()
	switch operation := sender.(type) {
	case *managepkg.Add[models.Product]:
		if operation.Model != nil {
			return service.ValidateCreate(operation.Model)
		}
	case *managepkg.Edit[models.Product]:
		if operation.Model != nil {
			return service.ValidateUpdate(operation.Model, operation.OldItem)
		}
	case *managepkg.Remove[models.Product]:
		if operation.Model != nil {
			return service.EnsureRemovable(operation.Model.ID)
		}
	}
	return nil
}

// SetBaseDataEnabled 供继承的通用启停命令调用。
func (own *ProductManage) SetBaseDataEnabled(id uint, enabled bool) (*models.Product, error) {
	return business.NewProductService().SetEnabled(id, enabled)
}
