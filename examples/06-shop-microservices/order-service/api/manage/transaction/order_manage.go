package transaction

import (
	"strconv"

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

func (own *OrderManage) OnCommandBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	var selected *models.Order
	switch operation := sender.(type) {
	case *CancelOrder:
		selected = operation.Model
	case *RefundOrder:
		selected = operation.Model
	default:
		return nil, nil, false
	}
	if selected == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	item, err := models.FindOrder(selected.ID)
	if err != nil || item == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	result, err := business.CancelOrder(item.UserID, item.ID, strconv.FormatUint(uint64(req.NewID()), 10))
	return result, err, true
}

func (*OrderManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "订单查询", true
}
