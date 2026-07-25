// Package basedata 提供 07 订单规则 Manage API。
package basedata

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// OrderRuleManage 管理共享远程权威库中的订单规则。
type OrderRuleManage struct {
	*BaseDataManage[models.OrderRule]
}

// NewOrderRuleManage 创建订单规则 Manage。
func NewOrderRuleManage() *OrderRuleManage {
	own := &OrderRuleManage{}
	own.BaseDataManage = NewBaseDataManage[models.OrderRule](own)
	return own
}

// Routers 返回订单规则 Manage 路由集合。
func (own *OrderRuleManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit}
}

// OnAddBefore 将新增订单规则写入共享远程权威库。
func (own *OrderRuleManage) OnAddBefore(operation *managepkg.Add[models.OrderRule], req servertypes.IRequest) (interface{}, error, bool) {
	if operation.Model == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	operation.Model.TraceID = req.GetTraceId()
	result, err := business.SaveOrderRule(operation.Model)
	return result, err, true
}

// OnEditBefore 将订单规则修改写入共享远程权威库。
func (own *OrderRuleManage) OnEditBefore(operation *managepkg.Edit[models.OrderRule], req servertypes.IRequest) (interface{}, error, bool) {
	if operation.Model == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	operation.Model.TraceID = req.GetTraceId()
	result, err := business.SaveOrderRule(operation.Model)
	return result, err, true
}

// ViewModel 定义订单规则管理视图。
func (*OrderRuleManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "订单规则管理", true
}
