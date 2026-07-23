package transaction

import (
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

const OrderManageMaxPageSize = 30

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
}

// OnSearchBefore 先继承业务数据层的只读查询规则，再针对订单预加载将每页上限收紧为 30。
func (own *OrderManage) OnSearchBefore(search *managepkg.Search[models.Order], req servertypes.IRequest) (interface{}, error, bool) {
	data, err, stop := own.BusinessManage.OnSearchBefore(search, req)
	if stop || err != nil {
		return data, err, stop
	}
	if search != nil && search.SearchItem != nil && search.SearchItem.Size > OrderManageMaxPageSize {
		search.SearchItem.Size = OrderManageMaxPageSize
	}
	return nil, nil, false
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
