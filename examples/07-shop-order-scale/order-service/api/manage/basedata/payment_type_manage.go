// Package basedata 提供 07 支付类型 Manage API。
package basedata

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// PaymentTypeManage 管理共享远程权威库中的支付类型。
type PaymentTypeManage struct {
	*BaseDataManage[models.PaymentType]
}

// NewPaymentTypeManage 创建支付类型 Manage。
func NewPaymentTypeManage() *PaymentTypeManage {
	own := &PaymentTypeManage{}
	own.BaseDataManage = NewBaseDataManage[models.PaymentType](own)
	return own
}

// Routers 返回支付类型 Manage 路由集合。
func (own *PaymentTypeManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit}
}

// OnAddBefore 将新增支付类型写入共享远程权威库。
func (own *PaymentTypeManage) OnAddBefore(operation *managepkg.Add[models.PaymentType], req servertypes.IRequest) (interface{}, error, bool) {
	if operation.Model == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	operation.Model.TraceID = req.GetTraceId()
	result, err := business.SavePaymentType(operation.Model)
	return result, err, true
}

// OnEditBefore 将支付类型修改写入共享远程权威库。
func (own *PaymentTypeManage) OnEditBefore(operation *managepkg.Edit[models.PaymentType], req servertypes.IRequest) (interface{}, error, bool) {
	if operation.Model == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	operation.Model.TraceID = req.GetTraceId()
	result, err := business.SavePaymentType(operation.Model)
	return result, err, true
}

// ViewModel 定义支付类型管理视图。
func (*PaymentTypeManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "支付类型管理", true
}
