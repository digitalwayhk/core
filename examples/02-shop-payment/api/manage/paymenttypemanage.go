package manage

import (
	"strings"

	"github.com/digitalwayhk/core/examples/02-shop-payment/business"
	"github.com/digitalwayhk/core/examples/02-shop-payment/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// PaymentTypeManage 演示支付类型 CRUD、引用保护和受控启停。
type PaymentTypeManage struct {
	*managepkg.ManageService[models.PaymentType]
	Enable  *EnablePaymentType
	Disable *DisablePaymentType
}

// NewPaymentTypeManage 创建支付类型管理服务和自定义命令。
func NewPaymentTypeManage() *PaymentTypeManage {
	own := &PaymentTypeManage{}
	own.ManageService = managepkg.NewManageService[models.PaymentType](own)
	own.Enable = NewEnablePaymentType(own)
	own.Disable = NewDisablePaymentType(own)
	return own
}

// Routers 暴露 CRUD 和启用、禁用命令。
func (own *PaymentTypeManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove, own.Enable, own.Disable}
}

// ParseAfter 规范化支付类型编码和名称。
func (own *PaymentTypeManage) ParseAfter(sender interface{}, _ servertypes.IRequest) error {
	var item *models.PaymentType
	switch operation := sender.(type) {
	case *managepkg.Add[models.PaymentType]:
		item = operation.Model
	case *managepkg.Edit[models.PaymentType]:
		item = operation.Model
	}
	if item != nil {
		item.Code = strings.ToLower(strings.TrimSpace(item.Code))
		item.Name = strings.TrimSpace(item.Name)
		item.Description = strings.TrimSpace(item.Description)
	}
	return nil
}

// ValidationAfter 调用业务层校验唯一性、稳定编码和引用删除保护。
func (own *PaymentTypeManage) ValidationAfter(sender interface{}, _ servertypes.IRequest) error {
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

// ViewModel 设置支付类型管理页面。
func (own *PaymentTypeManage) ViewModel(model *view.ViewModel) {
	model.Title = "支付类型管理"
	model.AutoLoad = true
}

// ViewFieldModel 配置支付类型字段和启用状态显示。
func (own *PaymentTypeManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	switch {
	case field.IsFieldOrTitle("Code"):
		field.Title = "支付编码"
		field.IsSearch = true
	case field.IsFieldOrTitle("Name"):
		field.Title = "支付名称"
		field.IsSearch = true
	case field.IsFieldOrTitle("Enabled"):
		field.Title = "启用状态"
		field.IsEdit = false
		field.ComBoxValue(0, "禁用")
		field.ComBoxValue(1, "启用")
	case field.IsFieldOrTitle("Description"):
		field.Title = "说明"
	}
}

// ViewCommandModel 把自定义命令显示为受控中文按钮。
func (own *PaymentTypeManage) ViewCommandModel(command *view.CommandModel) {
	command.Visible = true
	command.IsSelectRow = true
	command.IsAlert = true
	switch command.Name {
	case "EnablePaymentType":
		command.Title = "启用"
		command.Icon = "check"
	case "DisablePaymentType":
		command.Title = "禁用"
		command.Icon = "ban"
	}
}
