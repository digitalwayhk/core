// Package basedata 提供 07 供应商资料 Manage API。
package basedata

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

// SupplierManage 管理供应商服务本地权威库中的供应商资料。
type SupplierManage struct {
	*BaseDataManage[models.Supplier]
}

// NewSupplierManage 创建供应商资料 Manage。
func NewSupplierManage() *SupplierManage {
	own := &SupplierManage{}
	own.BaseDataManage = NewBaseDataManage[models.Supplier](own)
	return own
}

// Routers 返回供应商资料 Manage 路由集合。
func (own *SupplierManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit}
}

// ViewModel 定义供应商资料管理视图。
func (*SupplierManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "供应商资料管理", true
}
