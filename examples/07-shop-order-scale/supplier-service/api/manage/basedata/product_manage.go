// Package basedata 提供 07 商品资料 Manage API。
package basedata

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ProductManage 管理供应商服务本地权威库中的商品资料。
type ProductManage struct {
	*BaseDataManage[models.Product]
}

// NewProductManage 创建商品资料 Manage。
func NewProductManage() *ProductManage {
	own := &ProductManage{}
	own.BaseDataManage = NewBaseDataManage[models.Product](own)
	return own
}

// Routers 返回商品资料 Manage 路由集合。
func (own *ProductManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit}
}

// ViewModel 定义商品资料管理视图。
func (*ProductManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "商品资料管理", true
}
