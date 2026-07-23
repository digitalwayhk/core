// Package transaction 提供 07 订单统一管理查询 API。
package transaction

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

// OrderManage 查询共享远程权威库中的订单事实。
type OrderManage struct {
	*TransactionManage[models.Order]
}

// NewOrderManage 创建订单管理查询 Manage。
func NewOrderManage() *OrderManage {
	own := &OrderManage{}
	own.TransactionManage = NewTransactionManage[models.Order](own)
	return own
}

// Routers 返回订单管理路由集合。
func (own *OrderManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search}
}

// ViewModel 定义订单管理视图。
func (*OrderManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "订单查询", true
}
