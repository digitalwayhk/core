package manage

import (
	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
	"github.com/digitalwayhk/core/service/manage/view"
)

// OrderManage 继承业务数据只读能力并格式化订单状态。
type OrderManage struct {
	*BusinessManage[models.Order]
}

// NewOrderManage 创建只读订单管理服务。
func NewOrderManage() *OrderManage {
	own := &OrderManage{}
	own.BusinessManage = NewBusinessManage[models.Order](own)
	return own
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

// ViewFieldModel 先应用业务公共规则，再格式化订单和支付状态。
func (own *OrderManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.BusinessManage.ViewFieldModel(model, field)
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
