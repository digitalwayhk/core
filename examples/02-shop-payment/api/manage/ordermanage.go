package manage

import (
	"github.com/digitalwayhk/core/examples/02-shop-payment/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// OrderManage 提供只读订单管理和状态格式化。
type OrderManage struct {
	*managepkg.ManageService[models.Order]
}

// NewOrderManage 创建只读订单管理服务。
func NewOrderManage() *OrderManage {
	own := &OrderManage{}
	own.ManageService = managepkg.NewManageService[models.Order](own)
	return own
}

// Routers 只暴露 View 和 Search。
func (own *OrderManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search}
}

// ViewModel 设置订单管理页面。
func (own *OrderManage) ViewModel(model *view.ViewModel) {
	model.Title = "订单管理"
	model.AutoLoad = true
	if model.ViewField("PaymentStatus") == nil {
		field := &view.FieldModel{
			Field: "paymentStatus", PropField: "PaymentStatus", Title: "支付状态",
			Visible: true, IsEdit: false, IsSearch: true, Sorter: true, Type: "paymentstatus",
		}
		for _, status := range []models.PaymentStatus{models.PaymentStatusUnpaid, models.PaymentStatusPending, models.PaymentStatusPaid, models.PaymentStatusFailed, models.PaymentStatusRefunding, models.PaymentStatusRefunded} {
			field.ComBoxValue(int(status), status.String())
		}
		model.Fields = append(model.Fields, field)
	}
}

// ViewFieldModel 配置订单和支付状态中文显示。
func (own *OrderManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	field.IsEdit = false
	if field.IsFieldOrTitle("Status") {
		field.Title = "订单状态"
		for _, status := range []models.OrderStatus{models.OrderStatusNormal, models.OrderStatusCancelling, models.OrderStatusCancelled} {
			field.ComBoxValue(int(status), status.String())
		}
	}
	if field.IsFieldOrTitle("PaymentStatus") {
		field.Title = "支付状态"
		for _, status := range []models.PaymentStatus{models.PaymentStatusUnpaid, models.PaymentStatusPending, models.PaymentStatusPaid, models.PaymentStatusFailed, models.PaymentStatusRefunding, models.PaymentStatusRefunded} {
			field.ComBoxValue(int(status), status.String())
		}
	}
}
