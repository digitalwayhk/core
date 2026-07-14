package manage

import (
	"github.com/digitalwayhk/core/examples/01-simple-shop/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ProductManage 组装商品的查看、查询、新增、修改和删除管理路由。
type ProductManage struct {
	*managepkg.ManageService[models.Product]
}

// NewProductManage 创建商品管理服务并传入正确的 hook owner。
func NewProductManage() *ProductManage {
	own := &ProductManage{}
	own.ManageService = managepkg.NewManageService[models.Product](own)
	return own
}

// Routers 只暴露本示例需要的商品 CRUD，不启用状态提交与发布。
func (own *ProductManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove}
}

// ViewModel 设置管理界面的商品标题和自动加载行为。
func (own *ProductManage) ViewModel(model *view.ViewModel) {
	model.Title = "商品管理"
	model.AutoLoad = true
}
