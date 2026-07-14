package private

import (
	"testing"

	"github.com/digitalwayhk/core/examples/01-simple-shop/api/dto"
	"github.com/stretchr/testify/assert"
)

// TestGetOrdersResetClearsSubscriptionIdentity 验证路由归还对象池前清除 WebSocket 订阅身份。
func TestGetOrdersResetClearsSubscriptionIdentity(t *testing.T) {
	router := &GetOrders{}
	router.SetUserID("user-a", "name-a")
	assert.Equal(t, "user-a", router.GetUserID())

	router.Reset()
	assert.Empty(t, router.GetUserID())
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
