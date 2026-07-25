package private

import (
	"testing"

	"github.com/digitalwayhk/core/examples/01-simple-shop/api/dto"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
)

// TestGetOrdersUsesOnlyRequiredWebSocketContracts 验证订单订阅只实现实际需要的扩展接口。
func TestGetOrdersUsesOnlyRequiredWebSocketContracts(t *testing.T) {
	router := &GetOrders{}
	assert.Implements(t, (*servertypes.IWebSocketUserIdentity)(nil), router)
	assert.Implements(t, (*servertypes.IRouterHashKey)(nil), router)
	assert.Implements(t, (*servertypes.IWebSocketRouterNotice)(nil), router)

	_, hasLifecycleCallback := interface{}(router).(servertypes.IWebSocketRouter)
	assert.False(t, hasLifecycleCallback, "示例没有订阅组启停动作，应使用框架默认订阅管理")
	_, hasPoolReset := interface{}(router).(servertypes.IRouterResettable)
	assert.False(t, hasPoolReset, "独立订阅实例不进入请求池，不需要自定义 Reset")
	_, hasPoolCleanup := interface{}(router).(servertypes.IRouterCleanable)
	assert.False(t, hasPoolCleanup, "订阅实例释放后直接丢弃，不需要自定义 Clean")
}

// TestGetOrdersFiltersOrderResponse 验证 WebSocket 直接按订单 DTO 的用户字段进行隔离。
func TestGetOrdersFiltersOrderResponse(t *testing.T) {
	filter := &GetOrders{}
	subscription := &GetOrders{}
	subscription.SetUserID("user-a", "")

	accepted, message := filter.NoticeFiltersRouter(&dto.OrderResponse{UserID: "user-a", Action: "created"}, subscription)
	assert.True(t, accepted)
	assert.Equal(t, "created", message.(*dto.OrderResponse).Action)

	accepted, message = filter.NoticeFiltersRouter(&dto.OrderResponse{UserID: "user-b", Action: "created"}, subscription)
	assert.False(t, accepted)
	assert.Nil(t, message)
}
