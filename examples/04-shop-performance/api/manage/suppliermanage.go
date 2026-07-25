package manage

import (
	publicapi "github.com/digitalwayhk/core/examples/04-shop-performance/api/public"
	"github.com/digitalwayhk/core/examples/04-shop-performance/business"
	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
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

// ValidationAfter 先保留父级校验，再由后续业务实现追加引用保护。
func (own *SupplierManage) ValidationAfter(sender interface{}, req servertypes.IRequest) error {
	if err := own.BaseDataManage.ValidationAfter(sender, req); err != nil {
		return err
	}
	service := business.NewSupplierService()
	switch operation := sender.(type) {
	case *managepkg.Add[models.Supplier]:
		if operation.Model != nil {
			return service.ValidateCreate(operation.Model)
		}
	case *managepkg.Edit[models.Supplier]:
		if operation.Model != nil {
			return service.ValidateUpdate(operation.Model, operation.OldItem)
		}
	case *managepkg.Remove[models.Supplier]:
		if operation.Model != nil {
			return service.EnsureRemovable(operation.Model.ID)
		}
	}
	return nil
}

// SetBaseDataEnabled 供继承的通用启停命令调用。
func (own *SupplierManage) SetBaseDataEnabled(id uint, enabled bool) (*models.Supplier, error) {
	model, err := business.NewSupplierService().SetEnabled(id, enabled)
	if err == nil {
		publicapi.InvalidateSupplierCaches()
		invalidateOrderReferenceBestEffort("supplier_set_enabled")
	}
	return model, err
}

// DoAfter 在供应商增删改成功后清理供应商、商品查询及下单事实缓存。
func (own *SupplierManage) DoAfter(sender interface{}, req servertypes.IRequest) (interface{}, error) {
	publicapi.InvalidateSupplierCaches()
	invalidateOrderReferenceBestEffort("supplier_changed")
	return nil, nil
}
