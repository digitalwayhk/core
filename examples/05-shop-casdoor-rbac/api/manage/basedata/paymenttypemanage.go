package basedata

import (
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/business"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
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

// OnEditBefore 新增完全继承基础资料层；编辑时先执行父级校验，再保护已使用支付类型的编码稳定性。
func (own *PaymentTypeManage) OnEditBefore(operation *managepkg.Edit[models.PaymentType], req servertypes.IRequest) (interface{}, error, bool) {
	data, err, stop := own.BaseDataManage.OnEditBefore(operation, req)
	if stop || err != nil {
		return data, err, stop
	}
	if operation != nil && operation.Model != nil && operation.OldItem != nil {
		if err := business.NewPaymentTypeService().EnsureUsedCodeStable(operation.Model, operation.OldItem); err != nil {
			return nil, err, true
		}
	}
	return nil, nil, false
}

// SetBaseDataEnabled 供继承的通用启停命令调用。
func (own *PaymentTypeManage) SetBaseDataEnabled(id uint, enabled bool) (*models.PaymentType, error) {
	return business.NewPaymentTypeService().SetEnabled(id, enabled)
}
