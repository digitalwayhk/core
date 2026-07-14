package manage

import (
	"github.com/digitalwayhk/core/examples/01-simple-shop/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// OrderManage 组装只读订单管理路由，订单写操作必须经过用户私有 API。
type OrderManage struct {
	*managepkg.ManageService[models.Order]
}

// NewOrderManage 创建订单管理服务并传入正确的 hook owner。
func NewOrderManage() *OrderManage {
	own := &OrderManage{}
	own.ManageService = managepkg.NewManageService[models.Order](own)
	return own
}

// Routers 仅暴露 View 与 Search，避免管理端绕过订单所有权和通知逻辑。
func (own *OrderManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search}
}

// ViewModel 设置管理界面的订单标题和自动加载行为。
func (own *OrderManage) ViewModel(model *view.ViewModel) {
	model.Title = "订单管理"
	model.AutoLoad = true
}
