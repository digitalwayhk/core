package simpleshop_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrivateAPIs 验证下单、本人查询、本人删除、快照与每秒重复下单约束。
func TestPrivateAPIs(t *testing.T) {
	adminToken := suite.TokenFor(t, "private-admin", 1)
	userAToken := suite.TokenFor(t, "private-user-a", 0)
	userBToken := suite.TokenFor(t, "private-user-b", 0)
	productName := fmt.Sprintf("私有商品-%d", time.Now().UnixNano())
	product := suite.AddProduct(t, adminToken, productName, "39.80")
	productID := UintID(t, product.ID)

	for _, request := range []struct {
		method string
		path   string
		body   interface{}
	}{
		{http.MethodPost, "/api/shop/addorder", map[string]interface{}{"productID": productID, "quantity": 1}},
		{http.MethodGet, "/api/shop/getorders", nil},
		{http.MethodPost, "/api/shop/deleteorder", map[string]string{"id": "1"}},
	} {
		response := suite.RequestJSON(t, request.method, request.path, "", request.body)
		assert.Equal(t, http.StatusUnauthorized, response.HTTPStatus, request.path)
	}

	missing := suite.RequestJSON(t, http.MethodPost, "/api/shop/addorder", userAToken, map[string]interface{}{
		"productID": uint(999999999), "quantity": 1,
	})
	assert.Equal(t, http.StatusUnprocessableEntity, missing.HTTPStatus)
	assert.Equal(t, "商品不存在", missing.ErrorMessage)

	invalid := suite.RequestJSON(t, http.MethodPost, "/api/shop/addorder", userAToken, map[string]interface{}{
		"productID": productID, "quantity": 0,
	})
	assert.Equal(t, http.StatusBadRequest, invalid.HTTPStatus)
	assert.Equal(t, "订单数量必须大于 0", invalid.ErrorMessage)

	created := suite.RequestJSON(t, http.MethodPost, "/api/shop/addorder", userAToken, map[string]interface{}{
		"productID": productID, "quantity": 2,
	})
	require.True(t, created.Success, created.ErrorMessage)
	var order OrderDTO
	require.NoError(t, json.Unmarshal(created.Data, &order))
	assert.Equal(t, "private-user-a", order.UserID)
	assert.Equal(t, productName, order.ProductName)
	assert.Equal(t, "39.8", order.UnitPrice)
	assert.NotContains(t, string(created.Data), "hashCode")
	assert.NotContains(t, string(created.Data), "modelState")
	createdAt, err := time.Parse(time.RFC3339, order.CreatedAt)
	require.NoError(t, err)
	assert.Equal(t, 0, createdAt.Nanosecond())

	edit := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/edit", adminToken, map[string]interface{}{
		"id": product.ID, "name": productName + "-新名称", "price": "88.00",
	})
	require.True(t, edit.Success, edit.ErrorMessage)

	ordersA := suite.RequestJSON(t, http.MethodGet, "/api/shop/getorders", userAToken, nil)
	require.True(t, ordersA.Success, ordersA.ErrorMessage)
	var userAOrders []OrderDTO
	require.NoError(t, json.Unmarshal(ordersA.Data, &userAOrders))
	assert.Contains(t, OrderIDs(userAOrders), order.ID)
	for _, saved := range userAOrders {
		if saved.ID == order.ID {
			assert.Equal(t, productName, saved.ProductName)
			assert.Equal(t, "39.8", saved.UnitPrice)
		}
	}

	ordersB := suite.RequestJSON(t, http.MethodGet, "/api/shop/getorders", userBToken, nil)
	var userBOrders []OrderDTO
	require.NoError(t, json.Unmarshal(ordersB.Data, &userBOrders))
	assert.NotContains(t, OrderIDs(userBOrders), order.ID)

	forbidden := suite.RequestJSON(t, http.MethodPost, "/api/shop/deleteorder", userBToken, map[string]string{"id": order.ID})
	assert.Equal(t, http.StatusUnprocessableEntity, forbidden.HTTPStatus)
	assert.Equal(t, "订单不存在或无权操作", forbidden.ErrorMessage)

	duplicateProduct := suite.AddProduct(t, adminToken, fmt.Sprintf("秒级商品-%d", time.Now().UnixNano()), "9.90")
	waitForNextSecond()
	first := suite.RequestJSON(t, http.MethodPost, "/api/shop/addorder", userAToken, map[string]interface{}{
		"productID": UintID(t, duplicateProduct.ID), "quantity": 1,
	})
	require.True(t, first.Success, first.ErrorMessage)
	second := suite.RequestJSON(t, http.MethodPost, "/api/shop/addorder", userAToken, map[string]interface{}{
		"productID": UintID(t, duplicateProduct.ID), "quantity": 1,
	})
	assert.False(t, second.Success, "同一用户同一商品每秒只能下单一次")
	assert.Contains(t, second.ErrorMessage, "每秒只能购买一次")

	deleted := suite.RequestJSON(t, http.MethodPost, "/api/shop/deleteorder", userAToken, map[string]string{"id": order.ID})
	require.True(t, deleted.Success, deleted.ErrorMessage)
	assert.NotContains(t, string(deleted.Data), "hashCode")

	afterDelete := suite.RequestJSON(t, http.MethodGet, "/api/shop/getorders", userAToken, nil)
	require.NoError(t, json.Unmarshal(afterDelete.Data, &userAOrders))
	assert.NotContains(t, OrderIDs(userAOrders), order.ID)
}

// TestPrivateWebSocket 验证匿名订阅被拒绝，并且订单新增与删除事件只投递给当前用户。
func TestPrivateWebSocket(t *testing.T) {
	adminToken := suite.TokenFor(t, "ws-admin", 1)
	userAToken := suite.TokenFor(t, "ws-user-a", 0)
	userBToken := suite.TokenFor(t, "ws-user-b", 0)
	product := suite.AddProduct(t, adminToken, fmt.Sprintf("WebSocket 商品-%d", time.Now().UnixNano()), "12.50")

	unauthenticated, _, err := websocket.DefaultDialer.Dial(suite.WebSocketURL, nil)
	require.NoError(t, err)
	suite.WriteWebSocket(t, unauthenticated, "sub", "/api/shop/getorders", map[string]interface{}{})
	unauthenticatedReply := suite.ReadWebSocket(t, unauthenticated, 3*time.Second)
	assert.Equal(t, "error", unauthenticatedReply.Event)
	_ = unauthenticated.Close()

	connectionA := suite.ConnectAndSubscribe(t, userAToken)
	defer connectionA.Close()
	connectionB := suite.ConnectAndSubscribe(t, userBToken)
	defer connectionB.Close()
	messagesB := suite.StreamWebSocket(t, connectionB)

	created := suite.RequestJSON(t, http.MethodPost, "/api/shop/addorder", userAToken, map[string]interface{}{
		"productID": UintID(t, product.ID), "quantity": 3,
	})
	require.True(t, created.Success, created.ErrorMessage)
	createdEvent := suite.ReadOrderEvent(t, connectionA)
	assert.Equal(t, "created", createdEvent.Action)
	assert.Equal(t, "ws-user-a", createdEvent.Order.UserID)
	AssertNoOrderEvent(t, messagesB)

	deleted := suite.RequestJSON(t, http.MethodPost, "/api/shop/deleteorder", userAToken, map[string]string{"id": createdEvent.Order.ID})
	require.True(t, deleted.Success, deleted.ErrorMessage)
	deletedEvent := suite.ReadOrderEvent(t, connectionA)
	assert.Equal(t, "deleted", deletedEvent.Action)
	assert.Equal(t, createdEvent.Order.ID, deletedEvent.Order.ID)
	AssertNoOrderEvent(t, messagesB)
}

// waitForNextSecond 将重复下单测试放在同一秒开头，避免跨秒边界导致假失败。
func waitForNextSecond() {
	now := time.Now()
	time.Sleep(time.Until(now.Truncate(time.Second).Add(time.Second)) + 20*time.Millisecond)
}
