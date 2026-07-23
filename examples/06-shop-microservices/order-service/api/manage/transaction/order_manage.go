// 本文件提供当前服务交易域 Manage API 的查询、状态命令和受控操作能力。
package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

// OrderManage 定义本文件能力使用的核心结构。
type OrderManage struct {
	*TransactionManage[models.Order]
	Cancel *CancelOrder
	Refund *RefundOrder
}

// NewOrderManage 执行本文件能力对应的业务操作。
func NewOrderManage() *OrderManage {
	own := &OrderManage{}
	own.TransactionManage = NewTransactionManage[models.Order](own)
	own.Cancel, own.Refund = NewCancelOrder(own), NewRefundOrder(own)
	return own
}

// Routers 实现本类型在当前服务边界中的行为。
func (own *OrderManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Cancel, own.Refund}
}

func cancelSelectedOrder(selected *models.Order, traceID, eventID string) (interface{}, error) {
	if selected == nil {
		return nil, contract.ErrResourceNotFound
	}
	item, err := models.FindOrder(selected.ID)
	if err != nil || item == nil {
		return nil, contract.ErrResourceNotFound
	}
	return business.CancelOrder(item.UserID, item.ID, traceID, eventID)
}

// ViewModel 实现本类型在当前服务边界中的行为。
func (*OrderManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "订单查询", true
}
