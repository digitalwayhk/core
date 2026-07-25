// Package shoporderscale 验证 07 all-in-one 真实进程中的买家 WebSocket 订单订阅。
// 本文件覆盖真实 HTTP 登录、WebSocket 订阅、订单 pending 同步后的事件投递，以及其他买家的隔离边界。
package shoporderscale

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	userdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/user"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// TestUATBuyerWebSocketOrderSubscription 验证买家下单后只会由本人 WebSocket 订阅收到订单事件。
func TestUATBuyerWebSocketOrderSubscription(t *testing.T) {
	requireOrderMySQL(t)
	user, supplier := start07AllInOne(t)
	adminToken := supplier.TokenFor(t, "platform-admin", 1)
	productID := add07SupplierProduct(t, supplier, adminToken)

	buyerToken := user.TokenFor(t, "710001", 0)
	otherToken := user.TokenFor(t, "710002", 0)
	buyerWS := connect07BuyerOrdersWebSocket(t, user, buyerToken)
	defer buyerWS.Close()
	otherWS := connect07BuyerOrdersWebSocket(t, user, otherToken)
	defer otherWS.Close()
	otherEvents := user.StreamWebSocket(t, otherWS)

	created := create07BuyerOrder(t, user, buyerToken, productID)
	message := user.ReadWebSocket(t, buyerWS, 5*time.Second)
	var event orderdto.OrderChanged
	require.NoError(t, json.Unmarshal(message.Data, &event))
	require.Equal(t, created.OrderID, event.OrderID)
	require.Equal(t, uint(710001), event.UserID)

	select {
	case unexpected := <-otherEvents:
		t.Fatalf("其他买家不应收到订单 WebSocket 事件: %+v", unexpected)
	case <-time.After(300 * time.Millisecond):
	}
}

// start07AllInOne 启动 07 单进程三服务真实进程，并返回用户和供应商访问端口。
func start07AllInOne(t *testing.T) (*integration.Suite, *integration.Suite) {
	t.Helper()
	base, err := integration.StartProcess(integration.ProcessOptions{
		BuildPackage:     "./examples/07-shop-order-scale/main/all-in-one",
		BinaryName:       "shop-order-scale",
		TempPrefix:       "core-shop-order-scale-",
		ServiceCount:     4,
		ServiceIndex:     1,
		GRPCServiceCount: 4,
		Arguments:        []string{"-view", "0"},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if t.Failed() {
			base.PrintLog()
		}
		base.Stop()
	})
	wait07Ready(t, base)
	supplier := *base
	supplier.BaseURL = fmt.Sprintf("http://127.0.0.1:%d", base.BasePort+2)
	supplier.WebSocketURL = fmt.Sprintf("ws://127.0.0.1:%d/ws", base.BasePort+2)
	return base, &supplier
}

// wait07Ready 等待 user facade 可以通过真实服务链路查询商品列表。
func wait07Ready(t *testing.T, suite *integration.Suite) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := suite.DoJSON(http.MethodPost, "/api/shop-user/getproducts", "", map[string]interface{}{})
		if err == nil && response.Success {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("等待 07 all-in-one 启动超时")
}

// add07SupplierProduct 通过 supplier-service Manage API 准备一个可下单商品。
func add07SupplierProduct(t *testing.T, supplier *integration.Suite, adminToken string) uint {
	t.Helper()
	unique := strconv.FormatInt(time.Now().UnixNano(), 10)
	supplierResponse := supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/suppliermanage/add", adminToken, map[string]interface{}{
		"userID":      910001,
		"code":        "uat-supplier-" + unique,
		"name":        "07 UAT供应商",
		"description": "WebSocket UAT",
		"enabled":     true,
	})
	require.True(t, supplierResponse.Success, supplierResponse.ErrorMessage)
	supplierID := parse07ManageID(t, supplierResponse.Data)

	productResponse := supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/productmanage/add", adminToken, map[string]interface{}{
		"supplierID": supplierID,
		"code":       "uat-product-" + unique,
		"name":       "07 UAT商品",
		"price":      "19.50",
		"enabled":    true,
	})
	require.True(t, productResponse.Success, productResponse.ErrorMessage)
	return parse07ManageID(t, productResponse.Data)
}

// create07BuyerOrder 通过 user-service Private API 创建订单，返回 accepted 快照。
func create07BuyerOrder(t *testing.T, user *integration.Suite, buyerToken string, productID uint) orderdto.Order {
	t.Helper()
	response := user.RequestJSON(t, http.MethodPost, "/api/shop-user/addorder", buyerToken, map[string]interface{}{
		"productID": productID,
		"quantity":  2,
		"requestID": "uat-ws-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		"address": userdto.AddressSnapshot{
			AddressID:    1,
			ReceiverName: "WebSocket买家",
			Phone:        "13800000000",
			Province:     "广东省",
			City:         "深圳市",
			District:     "南山区",
			Detail:       "科技园",
		},
	})
	require.True(t, response.Success, response.ErrorMessage)
	var order orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &order))
	require.NotZero(t, order.OrderID)
	return order
}

// connect07BuyerOrdersWebSocket 登录并订阅 07 买家订单查询路由。
func connect07BuyerOrdersWebSocket(t *testing.T, user *integration.Suite, token string) *websocket.Conn {
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial(user.WebSocketURL, nil)
	require.NoError(t, err)
	user.WriteWebSocket(t, connection, "sub", "logon", map[string]string{"token": token})
	require.Equal(t, "success", user.ReadWebSocket(t, connection, 3*time.Second).Event)
	user.WriteWebSocket(t, connection, "sub", "/api/shop-user/getorders", map[string]interface{}{"page": 1, "size": 20})
	require.Equal(t, "sub", user.ReadWebSocket(t, connection, 3*time.Second).Event)
	return connection
}

// parse07ManageID 兼容 Manage 返回中 string 或 number 格式的 ID。
func parse07ManageID(t *testing.T, data json.RawMessage) uint {
	t.Helper()
	var raw struct {
		ID interface{} `json:"id"`
	}
	require.NoError(t, json.Unmarshal(data, &raw))
	switch value := raw.ID.(type) {
	case string:
		parsed, err := strconv.ParseUint(value, 10, 64)
		require.NoError(t, err)
		return uint(parsed)
	case float64:
		return uint(value)
	default:
		t.Fatalf("Manage 响应缺少可解析 ID: %s", string(data))
		return 0
	}
}
