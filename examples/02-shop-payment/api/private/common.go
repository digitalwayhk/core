package private

import (
	"github.com/digitalwayhk/core/examples/02-shop-payment/api/dto"
	"github.com/digitalwayhk/core/examples/02-shop-payment/models"
)

// notifyOrderChange 通过订单订阅路由发布用户观察通知。
func notifyOrderChange(response *dto.OrderResponse) {
	if response == nil {
		return
	}
	if info := (&GetOrders{}).RouterInfo(); info != nil {
		info.NoticeWebSocket(response)
	}
}

// NotifyOrderChange 供 Private 与 Manage 命令复用同一订单通知出口。
func NotifyOrderChange(action string, order *models.Order) *dto.OrderResponse {
	response := dto.NewOrderResponse(order)
	notifyOrderChange(response.WithAction(action))
	return response
}
