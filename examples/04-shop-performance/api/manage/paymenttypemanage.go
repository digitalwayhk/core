package manage

import (
	publicapi "github.com/digitalwayhk/core/examples/04-shop-performance/api/public"
	"github.com/digitalwayhk/core/examples/04-shop-performance/business"
	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// PaymentTypeManage 继承基础资料 CRUD、启停和字段规则。
type PaymentTypeManage struct {
	*BaseDataManage[models.PaymentType]
}

// NewPaymentTypeManage 创建以具体支付类型 Manage 为最终 owner 的管理服务。
func NewPaymentTypeManage() *PaymentTypeManage {
	own := &PaymentTypeManage{}
	own.BaseDataManage = NewBaseDataManage[models.PaymentType](own)
	return own
}

// ViewModel 设置支付类型管理页面。
func (own *PaymentTypeManage) ViewModel(model *view.ViewModel) {
	model.Title = "支付类型管理"
	model.AutoLoad = true
}

// ViewFieldModel 应用基础资料公共字段规则。
func (own *PaymentTypeManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.BaseDataManage.ViewFieldModel(model, field)
}

// ValidationAfter 先执行父级规则，再追加支付流水引用保护。
func (own *PaymentTypeManage) ValidationAfter(sender interface{}, req servertypes.IRequest) error {
	if err := own.BaseDataManage.ValidationAfter(sender, req); err != nil {
		return err
	}
	service := business.NewPaymentTypeService()
	switch operation := sender.(type) {
	case *managepkg.Add[models.PaymentType]:
		if operation.Model != nil {
			return service.ValidateCreate(operation.Model)
		}
	case *managepkg.Edit[models.PaymentType]:
		if operation.Model != nil && operation.OldItem != nil {
			return service.ValidateUpdate(operation.Model, operation.OldItem)
		}
	case *managepkg.Remove[models.PaymentType]:
		if operation.Model != nil {
			return service.EnsureRemovable(operation.Model.ID)
		}
	}
	return nil
}

// SetBaseDataEnabled 供继承的通用启停命令调用。
func (own *PaymentTypeManage) SetBaseDataEnabled(id uint, enabled bool) (*models.PaymentType, error) {
	model, err := business.NewPaymentTypeService().SetEnabled(id, enabled)
	if err == nil {
		publicapi.InvalidatePaymentTypeCache()
	}
	return model, err
}

// DoAfter 在支付类型增删改成功后清理可用支付类型缓存。
func (own *PaymentTypeManage) DoAfter(sender interface{}, req servertypes.IRequest) (interface{}, error) {
	publicapi.InvalidatePaymentTypeCache()
	return nil, nil
}
