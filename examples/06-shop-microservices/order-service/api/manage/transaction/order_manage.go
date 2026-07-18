package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

type OrderManage struct {
	*TransactionManage[models.Order]
	Cancel *CancelOrder
	Refund *RefundOrder
}

func NewOrderManage() *OrderManage {
	own := &OrderManage{}
	own.TransactionManage = NewTransactionManage[models.Order](own)
	own.Cancel, own.Refund = NewCancelOrder(own), NewRefundOrder(own)
	return own
}

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

func (*OrderManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "订单查询", true
}
