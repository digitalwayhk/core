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

func NewOrderManage() *OrderManage {
	own := &OrderManage{}
	own.TransactionManage = NewTransactionManage[models.SupplierOrder](own)
	return own
}

func (own *OrderManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search}
}

func (*OrderManage) SupplierOwnerColumn() string { return "SupplierID" }

func (*OrderManage) ViewModel(model *view.ViewModel) {
	model.Title = "供应商订单查询"
	model.AutoLoad = true
}
