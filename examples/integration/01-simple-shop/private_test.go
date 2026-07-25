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

// TestPrivateAPIs 按 API 运行全部 Private 集成测试。
func TestPrivateAPIs(t *testing.T) {
	t.Run("AddOrder", testAddOrderAPI)
	t.Run("GetOrders", testGetOrdersAPI)
	t.Run("DeleteOrder", testDeleteOrderAPI)
	t.Run("GetOrdersWebSocket", testGetOrdersWebSocketAPI)
}

// testAddOrderAPI 验证下单认证、参数、商品存在性、响应模型与每秒重复下单约束。
func testAddOrderAPI(t *testing.T) {
	adminToken := suite.TokenFor(t, "add-order-admin", 1)
	userToken := suite.TokenFor(t, "add-order-user", 0)
	productName := fmt.Sprintf("下单商品-%d", time.Now().UnixNano())
	product := suite.AddProduct(t, adminToken, productName, "39.80")
	productID := UintID(t, product.ID)

	unauthorized := suite.RequestJSON(t, http.MethodPost, "/api/shop/addorder", "", map[string]interface{}{"productID": productID, "quantity": 1})
	assert.Equal(t, http.StatusUnauthorized, unauthorized.HTTPStatus)

	missing := suite.RequestJSON(t, http.MethodPost, "/api/shop/addorder", userToken, map[string]interface{}{
		"productID": uint(999999999), "quantity": 1,
	})
	assert.Equal(t, http.StatusUnprocessableEntity, missing.HTTPStatus)
	assert.Equal(t, "商品不存在", missing.ErrorMessage)

	invalid := suite.RequestJSON(t, http.MethodPost, "/api/shop/addorder", userToken, map[string]interface{}{
		"productID": productID, "quantity": 0,
	})
	assert.Equal(t, http.StatusBadRequest, invalid.HTTPStatus)
	assert.Equal(t, "订单数量必须大于 0", invalid.ErrorMessage)

	created := suite.RequestJSON(t, http.MethodPost, "/api/shop/addorder", userToken, map[string]interface{}{
		"productID": productID, "quantity": 2,
	})
	require.True(t, created.Success, created.ErrorMessage)
	assert.NotContains(t, string(created.Data), `"action"`)
	var order OrderDTO
	require.NoError(t, json.Unmarshal(created.Data, &order))
	assert.Equal(t, "add-order-user", order.UserID)
	assert.Equal(t, productName, order.ProductName)
	assert.Equal(t, "39.8", order.UnitPrice)
	assert.NotContains(t, string(created.Data), "hashCode")
	assert.NotContains(t, string(created.Data), "modelState")
	createdAt, err := time.Parse(time.RFC3339, order.CreatedAt)
	require.NoError(t, err)
	assert.Equal(t, 0, createdAt.Nanosecond())

	duplicateProduct := suite.AddProduct(t, adminToken, fmt.Sprintf("秒级商品-%d", time.Now().UnixNano()), "9.90")
	waitForNextSecond()
	first := suite.AddOrder(t, userToken, duplicateProduct.ID, 1)
	require.NotEmpty(t, first.ID)
	second := suite.RequestJSON(t, http.MethodPost, "/api/shop/addorder", userToken, map[string]interface{}{
		"productID": UintID(t, duplicateProduct.ID), "quantity": 1,
	})
	assert.False(t, second.Success, "同一用户同一商品每秒只能下单一次")
	assert.Contains(t, second.ErrorMessage, "每秒只能购买一次")
}

// testGetOrdersAPI 验证订单查询认证、用户隔离和商品快照。
func testGetOrdersAPI(t *testing.T) {
	adminToken := suite.TokenFor(t, "get-orders-admin", 1)
	userAToken := suite.TokenFor(t, "get-orders-user-a", 0)
	userBToken := suite.TokenFor(t, "get-orders-user-b", 0)
	productName := fmt.Sprintf("订单查询商品-%d", time.Now().UnixNano())
	product := suite.AddProduct(t, adminToken, productName, "39.80")
	order := suite.AddOrder(t, userAToken, product.ID, 2)

	unauthorized := suite.RequestJSON(t, http.MethodGet, "/api/shop/getorders", "", nil)
	assert.Equal(t, http.StatusUnauthorized, unauthorized.HTTPStatus)

	edited := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/edit", adminToken, map[string]interface{}{
		"id": product.ID, "name": productName + "-新名称", "price": "88.00",
	})
	require.True(t, edited.Success, edited.ErrorMessage)

	userAOrders := suite.GetOrders(t, userAToken)
	assert.Contains(t, OrderIDs(userAOrders), order.ID)
	for _, saved := range userAOrders {
		if saved.ID == order.ID {
			assert.Equal(t, productName, saved.ProductName)
			assert.Equal(t, "39.8", saved.UnitPrice)
		}
	}
	assert.NotContains(t, OrderIDs(suite.GetOrders(t, userBToken)), order.ID)
}

// testDeleteOrderAPI 验证删除认证、订单所有权和物理删除结果。
func testDeleteOrderAPI(t *testing.T) {
	adminToken := suite.TokenFor(t, "delete-order-admin", 1)
	userAToken := suite.TokenFor(t, "delete-order-user-a", 0)
	userBToken := suite.TokenFor(t, "delete-order-user-b", 0)
	product := suite.AddProduct(t, adminToken, fmt.Sprintf("删单商品-%d", time.Now().UnixNano()), "20.00")
	order := suite.AddOrder(t, userAToken, product.ID, 1)

	unauthorized := suite.RequestJSON(t, http.MethodPost, "/api/shop/deleteorder", "", map[string]string{"id": order.ID})
	assert.Equal(t, http.StatusUnauthorized, unauthorized.HTTPStatus)

	forbidden := suite.RequestJSON(t, http.MethodPost, "/api/shop/deleteorder", userBToken, map[string]string{"id": order.ID})
	assert.Equal(t, http.StatusUnprocessableEntity, forbidden.HTTPStatus)
	assert.Equal(t, "订单不存在或无权操作", forbidden.ErrorMessage)

	deleted := suite.RequestJSON(t, http.MethodPost, "/api/shop/deleteorder", userAToken, map[string]string{"id": order.ID})
	require.True(t, deleted.Success, deleted.ErrorMessage)
	assert.NotContains(t, string(deleted.Data), `"action"`)
	assert.NotContains(t, string(deleted.Data), "hashCode")
	assert.NotContains(t, OrderIDs(suite.GetOrders(t, userAToken)), order.ID)
}

// testGetOrdersWebSocketAPI 验证匿名订阅被拒绝，且订单新增与删除事件只投递给当前用户。
func testGetOrdersWebSocketAPI(t *testing.T) {
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
	assert.Equal(t, "ws-user-a", createdEvent.UserID)
	AssertNoOrderEvent(t, messagesB)

	deleted := suite.RequestJSON(t, http.MethodPost, "/api/shop/deleteorder", userAToken, map[string]string{"id": createdEvent.ID})
	require.True(t, deleted.Success, deleted.ErrorMessage)
	deletedEvent := suite.ReadOrderEvent(t, connectionA)
	assert.Equal(t, "deleted", deletedEvent.Action)
	assert.Equal(t, createdEvent.ID, deletedEvent.ID)
	AssertNoOrderEvent(t, messagesB)
}

// waitForNextSecond 将重复下单测试放在同一秒开头，避免跨秒边界导致假失败。
func waitForNextSecond() {
	now := time.Now()
	time.Sleep(time.Until(now.Truncate(time.Second).Add(time.Second)) + 20*time.Millisecond)
}
