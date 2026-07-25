package transaction

import (
	privateapi "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/private"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/business"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

// ConfirmPayment 是支付流水管理页的确认支付命令。
type ConfirmPayment struct {
	managepkg.Operation[models.PaymentRecord]
}

// NewConfirmPayment 创建绑定 Manage owner 的确认支付命令。
func NewConfirmPayment(instance interface{}) *ConfirmPayment {
	return &ConfirmPayment{Operation: managepkg.NewOperation[models.PaymentRecord](instance)}
}

// New 为请求创建独立命令实例。
func (own *ConfirmPayment) New(instance interface{}) servertypes.IRouter {
	return NewConfirmPayment(instance)
}

// Validation 校验选中支付流水。
func (own *ConfirmPayment) Validation(servertypes.IRequest) error {
	return validatePaymentCommand(own.Model)
}

// Do 确认支付并通知订单用户。
func (own *ConfirmPayment) Do(servertypes.IRequest) (interface{}, error) {
	change, err := business.NewPaymentService().ConfirmPayment(own.Model.ID)
	if err != nil {
		return nil, err
	}
	return privateapi.NotifyOrderChange(change.Action, change.Order), nil
}

// RouterInfo 注册 Manage 自定义命令路径。
func (own *ConfirmPayment) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }

// FailPayment 是支付流水管理页的支付失败命令。
type FailPayment struct {
	managepkg.Operation[models.PaymentRecord]
}

// NewFailPayment 创建绑定 Manage owner 的支付失败命令。
func NewFailPayment(instance interface{}) *FailPayment {
	return &FailPayment{Operation: managepkg.NewOperation[models.PaymentRecord](instance)}
}

// New 为请求创建独立命令实例。
func (own *FailPayment) New(instance interface{}) servertypes.IRouter {
	return NewFailPayment(instance)
}

// Validation 校验选中支付流水。
func (own *FailPayment) Validation(servertypes.IRequest) error {
	return validatePaymentCommand(own.Model)
}

// Do 标记支付失败并通知订单用户。
func (own *FailPayment) Do(servertypes.IRequest) (interface{}, error) {
	change, err := business.NewPaymentService().FailPayment(own.Model.ID)
	if err != nil {
		return nil, err
	}
	return privateapi.NotifyOrderChange(change.Action, change.Order), nil
}

// RouterInfo 注册 Manage 自定义命令路径。
func (own *FailPayment) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }

// ConfirmRefund 是支付流水管理页的确认退款命令。
type ConfirmRefund struct {
	managepkg.Operation[models.PaymentRecord]
}

// NewConfirmRefund 创建绑定 Manage owner 的确认退款命令。
func NewConfirmRefund(instance interface{}) *ConfirmRefund {
	return &ConfirmRefund{Operation: managepkg.NewOperation[models.PaymentRecord](instance)}
}

// New 为请求创建独立命令实例。
func (own *ConfirmRefund) New(instance interface{}) servertypes.IRouter {
	return NewConfirmRefund(instance)
}

// Validation 校验选中支付流水。
func (own *ConfirmRefund) Validation(servertypes.IRequest) error {
	return validatePaymentCommand(own.Model)
}

// Do 确认退款并通知订单用户。
func (own *ConfirmRefund) Do(servertypes.IRequest) (interface{}, error) {
	change, err := business.NewPaymentService().ConfirmRefund(own.Model.ID)
	if err != nil {
		return nil, err
	}
	return privateapi.NotifyOrderChange(change.Action, change.Order), nil
}

// RouterInfo 注册 Manage 自定义命令路径。
func (own *ConfirmRefund) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }

// validatePaymentCommand 统一校验支付流水命令的选中行。
func validatePaymentCommand(model *models.PaymentRecord) error {
	if model == nil || model.ID == 0 {
		return models.NewValidationError("请选择支付流水")
	}
	return nil
}
