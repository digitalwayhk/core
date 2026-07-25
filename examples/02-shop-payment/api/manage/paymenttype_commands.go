package manage

import (
	"github.com/digitalwayhk/core/examples/02-shop-payment/business"
	"github.com/digitalwayhk/core/examples/02-shop-payment/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

// EnablePaymentType 是支付类型管理页的启用命令。
type EnablePaymentType struct {
	managepkg.Operation[models.PaymentType]
}

// NewEnablePaymentType 创建绑定 Manage owner 的启用命令。
func NewEnablePaymentType(instance interface{}) *EnablePaymentType {
	return &EnablePaymentType{Operation: managepkg.NewOperation[models.PaymentType](instance)}
}

// New 为请求创建独立命令实例。
func (own *EnablePaymentType) New(instance interface{}) servertypes.IRouter {
	return NewEnablePaymentType(instance)
}

// Validation 校验选中支付类型。
func (own *EnablePaymentType) Validation(servertypes.IRequest) error {
	if own.Model == nil || own.Model.ID == 0 {
		return models.NewValidationError("请选择支付类型")
	}
	return nil
}

// Do 调用业务层启用支付类型。
func (own *EnablePaymentType) Do(servertypes.IRequest) (interface{}, error) {
	return business.NewPaymentTypeService().Enable(own.Model.ID)
}

// RouterInfo 注册 Manage 自定义命令路径。
func (own *EnablePaymentType) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }

// DisablePaymentType 是支付类型管理页的禁用命令。
type DisablePaymentType struct {
	managepkg.Operation[models.PaymentType]
}

// NewDisablePaymentType 创建绑定 Manage owner 的禁用命令。
func NewDisablePaymentType(instance interface{}) *DisablePaymentType {
	return &DisablePaymentType{Operation: managepkg.NewOperation[models.PaymentType](instance)}
}

// New 为请求创建独立命令实例。
func (own *DisablePaymentType) New(instance interface{}) servertypes.IRouter {
	return NewDisablePaymentType(instance)
}

// Validation 校验选中支付类型。
func (own *DisablePaymentType) Validation(servertypes.IRequest) error {
	if own.Model == nil || own.Model.ID == 0 {
		return models.NewValidationError("请选择支付类型")
	}
	return nil
}

// Do 调用业务层禁用支付类型。
func (own *DisablePaymentType) Do(servertypes.IRequest) (interface{}, error) {
	return business.NewPaymentTypeService().Disable(own.Model.ID)
}

// RouterInfo 注册 Manage 自定义命令路径。
func (own *DisablePaymentType) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
