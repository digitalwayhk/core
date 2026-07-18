// 本文件验证用户服务 Private API 的买家订单闭环、缓存和 WebSocket 边界。
package private

import (
	"testing"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	"github.com/stretchr/testify/require"
)

// TestAddOrderRequiresClientRequestID 验证当前场景的业务闭环和边界行为。
func TestAddOrderRequiresClientRequestID(t *testing.T) {
	api := &AddOrder{ProductID: 1, Quantity: 1, AddressID: 1}
	err := api.Validation(nil)
	require.ErrorContains(t, err, "requestID")
}

// TestGetOrdersNoticeOnlyMatchesNumericUserID 验证当前场景的业务闭环和边界行为。
func TestGetOrdersNoticeOnlyMatchesNumericUserID(t *testing.T) {
	subscription := &GetOrders{subscriptionUserID: 20}
	match, _ := subscription.NoticeFiltersRouter(&event.OrderChanged{UserID: 20}, subscription)
	other, _ := subscription.NoticeFiltersRouter(&event.OrderChanged{UserID: 21}, subscription)
	require.True(t, match)
	require.False(t, other)
}
