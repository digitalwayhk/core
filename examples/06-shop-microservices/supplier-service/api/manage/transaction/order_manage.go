// 本文件提供当前服务交易域 Manage API 的查询、状态命令和受控操作能力。
package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

// OrderManage 只提供供应商订单投影的查询能力。
type OrderManage struct {
	*TransactionManage[models.SupplierOrder]
}

// NewOrderManage 执行本文件能力对应的业务操作。
func NewOrderManage() *OrderManage {
	own := &OrderManage{}
	own.TransactionManage = NewTransactionManage[models.SupplierOrder](own)
	return own
}

// Routers 实现本类型在当前服务边界中的行为。
func (own *OrderManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search}
}

// SupplierOwnerColumn 实现本类型在当前服务边界中的行为。
func (*OrderManage) SupplierOwnerColumn() string { return "SupplierID" }

// ViewModel 实现本类型在当前服务边界中的行为。
func (*OrderManage) ViewModel(model *view.ViewModel) {
	model.Title = "供应商订单查询"
	model.AutoLoad = true
}
