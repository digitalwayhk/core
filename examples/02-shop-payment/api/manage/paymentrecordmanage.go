package manage

import (
	"github.com/digitalwayhk/core/examples/02-shop-payment/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// PaymentRecordManage 提供只读流水查询和受控状态命令。
type PaymentRecordManage struct {
	*managepkg.ManageService[models.PaymentRecord]
	Confirm *ConfirmPayment
	Fail    *FailPayment
	Refund  *ConfirmRefund
}

// NewPaymentRecordManage 创建支付流水管理服务和状态命令。
func NewPaymentRecordManage() *PaymentRecordManage {
	own := &PaymentRecordManage{}
	own.ManageService = managepkg.NewManageService[models.PaymentRecord](own)
	own.Confirm = NewConfirmPayment(own)
	own.Fail = NewFailPayment(own)
	own.Refund = NewConfirmRefund(own)
	return own
}

// Routers 只暴露查询和三个受控命令。
func (own *PaymentRecordManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Confirm, own.Fail, own.Refund}
}

// ViewModel 设置支付流水管理页面。
func (own *PaymentRecordManage) ViewModel(model *view.ViewModel) {
	model.Title = "支付流水管理"
	model.AutoLoad = true
}

// ViewFieldModel 配置只读支付流水和状态中文显示。
func (own *PaymentRecordManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	field.IsEdit = false
	if field.IsFieldOrTitle("Status") {
		field.Title = "支付状态"
		for _, status := range []models.PaymentStatus{models.PaymentStatusPending, models.PaymentStatusPaid, models.PaymentStatusFailed, models.PaymentStatusRefunding, models.PaymentStatusRefunded} {
			field.ComBoxValue(int(status), status.String())
		}
	}
	if field.IsFieldOrTitle("Amount") {
		field.Title = "支付金额"
		field.Precision = 2
	}
}

// ViewCommandModel 配置支付结果处理按钮。
func (own *PaymentRecordManage) ViewCommandModel(command *view.CommandModel) {
	command.Visible = true
	command.IsSelectRow = true
	command.IsAlert = true
	switch command.Name {
	case "ConfirmPayment":
		command.Title = "确认支付"
		command.Icon = "check"
	case "FailPayment":
		command.Title = "支付失败"
		command.Icon = "x"
	case "ConfirmRefund":
		command.Title = "确认退款"
		command.Icon = "rotate-ccw"
	}
}
