package private

import (
	"strings"

	"github.com/digitalwayhk/core/examples/04-shop-performance/api/dto"
	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
)

// notifyOrderChange 通过订单订阅路由发布用户观察通知。
func notifyOrderChange(response *dto.OrderResponse) {
	if response == nil {
		return
	}
	InvalidateOrderCache(response.UserID)
	if info := (&GetOrders{}).RouterInfo(); info != nil {
		info.NoticeWebSocket(response)
	}
}

// InvalidateOrderCache 只清理指定认证用户的订单查询缓存。
func InvalidateOrderCache(userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	if info := (&GetOrders{}).RouterInfo(); info != nil {
		info.FailureCache(&GetOrders{requestUserID: userID})
	}
}

// NotifyOrderChange 供 Private 与 Manage 命令复用同一订单通知出口。
func NotifyOrderChange(action string, order *models.Order) *dto.OrderResponse {
	response := dto.NewOrderResponse(order)
	notifyOrderChange(response.WithAction(action))
	return response
}
