package simpleshop_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// removedCallEvent 是已删除的 WebSocket `call` 事件名。它曾经绕过整条 HTTP 认证
// 中间件链，直接用客户端给的 channel 查路由并执行 ExecDo。保留这个常量是为了让
// 回归测试继续从外部真实拨号，确认该事件不会以任何形式被重新接上。
const removedCallEvent = "call"

// unsupportedEventPrefix 是框架对未识别事件的固定应答前缀。
const unsupportedEventPrefix = "不支持的事件类型: "

// TestWebSocketCallEnforcesAuthBoundary 断言未认证的 WebSocket 会话无法借由
// 非订阅事件进入 Manage 与 Private 认证域。WebSocket 只面向最终外部用户：
// Manage 路由只接受 Manage Token，Private 路由只接受已登录会话身份。因此未认证
// 消息既不得执行路由、不得返回业务数据，也不得产生副作用。
func TestWebSocketCallEnforcesAuthBoundary(t *testing.T) {
	t.Run("ManageRemoveOnFreshSession", testWebSocketCallManageRemoveOnFreshSession)
	t.Run("ManageRemoveAfterPublicSubscribe", testWebSocketCallManageRemoveAfterPublicSubscribe)
	t.Run("ManageSearchRejected", testWebSocketCallManageSearchRejected)
	t.Run("PrivateRouteRejectedBeforeHandler", testWebSocketCallPrivateRouteRejected)
}

// testWebSocketCallManageRemoveOnFreshSession 验证刚建立、从未登录也从未订阅的
// 会话不能删除 Manage 数据。这条路径此前会在服务端解引用空 ServiceRouter 而恐慌
// 并掐断连接，所以这里额外要求服务端正常应答且连接仍可继续使用。
func testWebSocketCallManageRemoveOnFreshSession(t *testing.T) {
	connection := dialUnauthenticatedWebSocket(t)
	assertManageRemoveRejected(t, "ws-call-fresh", connection)
	requireConnectionStillUsable(t, connection)
}

// testWebSocketCallManageRemoveAfterPublicSubscribe 验证未认证会话先做一次
// 合法的公开路由订阅后，仍然不能删除 Manage 数据。
func testWebSocketCallManageRemoveAfterPublicSubscribe(t *testing.T) {
	assertManageRemoveRejected(t, "ws-call-subscribed", dialUnauthenticatedWebSocketWithRouter(t))
}

// assertManageRemoveRejected 用真实管理令牌准备一件商品，再以未认证会话对
// Manage Remove 发送 call，要求请求不被执行且商品仍然存在。
func assertManageRemoveRejected(t *testing.T, prefix string, connection *websocket.Conn) {
	t.Helper()
	adminToken := suite.TokenFor(t, prefix+"-admin", 1)
	productName := fmt.Sprintf("未认证删除商品-%s-%d", prefix, time.Now().UnixNano())
	product := suite.AddProduct(t, adminToken, productName, "19.90")

	message := callWithoutAuthentication(t, connection, "/api/manage/shop/productmanage/remove", map[string]interface{}{
		"model": map[string]interface{}{"id": product.ID, "name": productName, "price": "19.90"},
	})
	requireRouteNotExecuted(t, message)

	remaining := suite.GetProducts(t, "?id="+product.ID)
	require.Len(t, remaining, 1, "未认证的 WebSocket 消息不得删除 Manage 数据")
	require.Equal(t, productName, remaining[0].Name)
}

// testWebSocketCallManageSearchRejected 验证未认证会话不能读取只对管理员开放的
// Manage 列表数据。
func testWebSocketCallManageSearchRejected(t *testing.T) {
	adminToken := suite.TokenFor(t, "ws-call-search-admin", 1)
	productName := fmt.Sprintf("未认证查询商品-%d", time.Now().UnixNano())
	suite.AddProduct(t, adminToken, productName, "29.90")

	connection := dialUnauthenticatedWebSocketWithRouter(t)
	message := callWithoutAuthentication(t, connection, "/api/manage/shop/productmanage/search", map[string]interface{}{
		"SearchItem": map[string]interface{}{"page": 1, "size": 100},
	})
	requireRouteNotExecuted(t, message)
	require.NotContains(t, string(message.Data), productName, "未认证的 WebSocket 消息不得读取 Manage 数据")
}

// testWebSocketCallPrivateRouteRejected 验证未认证会话既不能用已删除的 call
// 事件执行 Private 路由，也不能用仍然存在的 sub 事件带着空 UID 进入业务处理器。
// sub 是删除 call 之后唯一还会执行路由代码的事件，它必须在认证层就被拒绝。
func testWebSocketCallPrivateRouteRejected(t *testing.T) {
	connection := dialUnauthenticatedWebSocketWithRouter(t)
	message := callWithoutAuthentication(t, connection, "/api/shop/getorders", map[string]interface{}{})
	requireRouteNotExecuted(t, message)

	suite.WriteWebSocket(t, connection, "sub", "/api/shop/getorders", map[string]interface{}{})
	subscribe := suite.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "error", subscribe.Event, string(subscribe.Data))
	require.Contains(t, string(subscribe.Data), "authentication failed",
		"Private 路由必须在认证层被拒绝")
	require.NotContains(t, string(subscribe.Data), "用户身份无效",
		"Private 路由必须在认证层被拒绝，不得带着空 UID 进入业务校验")
}

// dialUnauthenticatedWebSocket 建立一个从不执行 logon 的 WebSocket 会话。
func dialUnauthenticatedWebSocket(t *testing.T) *websocket.Conn {
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial(suite.WebSocketURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	return connection
}

// dialUnauthenticatedWebSocketWithRouter 在未认证会话上先完成一次公开路由订阅。
// 订阅公开路由是未认证客户端本就允许的动作，它让会话进入持有完整订阅状态的形态，
// 从而覆盖攻击者能够到达的最完整会话状态。
func dialUnauthenticatedWebSocketWithRouter(t *testing.T) *websocket.Conn {
	t.Helper()
	connection := dialUnauthenticatedWebSocket(t)
	suite.WriteWebSocket(t, connection, "sub", "/api/shop/getproducts", map[string]interface{}{})
	subscribed := suite.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "sub", subscribed.Event, string(subscribed.Data))
	require.Equal(t, "/api/shop/getproducts", subscribed.Channel)
	return connection
}

// callWithoutAuthentication 以未认证会话发送已删除的 call 事件并读取应答。
// 服务端必须应答；静默丢弃或断开连接都说明消息进入了不该进入的处理路径。
func callWithoutAuthentication(t *testing.T, connection *websocket.Conn, channel string, data interface{}) integration.WebSocketMessage {
	t.Helper()
	suite.WriteWebSocket(t, connection, removedCallEvent, channel, data)
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, raw, err := connection.ReadMessage()
	require.NoError(t, err, "服务端必须应答已删除的 %s 事件，而不是恐慌或断开连接", removedCallEvent)
	t.Logf("未认证 %s %s 的服务端应答: %s", removedCallEvent, channel, raw)
	var message integration.WebSocketMessage
	require.NoError(t, json.Unmarshal(raw, &message), string(raw))
	return message
}

// requireRouteNotExecuted 断言服务端把消息当作未识别事件拒绝，路由从未被执行。
// 应答必须是 error 事件且内容恰好是「不支持的事件类型」，这样既排除了返回业务
// 结果外壳，也排除了路由内部错误——后者同样意味着未认证请求已经进入了路由。
func requireRouteNotExecuted(t *testing.T, message integration.WebSocketMessage) {
	t.Helper()
	require.Equal(t, "error", message.Event, string(message.Data))
	var reason string
	require.NoError(t, json.Unmarshal(message.Data, &reason), string(message.Data))
	require.Equal(t, unsupportedEventPrefix+removedCallEvent, reason,
		"未认证消息必须在事件分发处被拒绝，不得进入任何路由")
}

// requireConnectionStillUsable 用一次合法的公开路由订阅确认连接没有被掐断。
func requireConnectionStillUsable(t *testing.T, connection *websocket.Conn) {
	t.Helper()
	suite.WriteWebSocket(t, connection, "sub", "/api/shop/getproducts", map[string]interface{}{})
	subscribed := suite.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "sub", subscribed.Event, string(subscribed.Data))
	require.Equal(t, "/api/shop/getproducts", subscribed.Channel)
}
