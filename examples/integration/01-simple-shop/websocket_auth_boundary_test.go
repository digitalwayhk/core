package simpleshop_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	shopmanage "github.com/digitalwayhk/core/examples/01-simple-shop/api/manage"
	integration "github.com/digitalwayhk/core/examples/integration"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
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
// 框架对单个 IP 的握手限速为每秒 5 次、突发 10 次，本文件的用例逐个新建连接，
// 这里按补充速率放行，避免边界断言被 429 握手失败掩盖。
func dialUnauthenticatedWebSocket(t *testing.T) *websocket.Conn {
	t.Helper()
	time.Sleep(250 * time.Millisecond)
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

// 三个认证域各自的代表频道。Manage 与 ServerManage 的路由都能被 ServiceRouter.GetRouter
// 解析到，所以它们都是 `sub` 事件可以指名的合法 channel。
const (
	manageSearchChannel      = "/api/manage/shop/productmanage/search"
	manageRemoveChannel      = "/api/manage/shop/productmanage/remove"
	serverManageAuthChannel  = "/api/internal/openapi"
	serverManageOpenChannel  = "/api/servermanage/runtimetopology"
	privateSubscribeChannel  = "/api/shop/getorders"
	authenticationFailedText = "authentication failed"
)

// TestWebSocketSubscribeEnforcesAuthDomain 断言 `sub`——删除 `call` 之后 `/ws` 上唯一
// 会执行路由代码的事件——不会成为跨认证域的入口。
//
// 框架有三个互不相同的认证域：用户 Token 只进 Private、Manage Token 只进 Manage、
// ServerManage Token 只进 ServerManage。HTTP 侧由 resolveRouteAuthPolicy 按路由所在的
// 表选择对应密钥与 AuthType，隔离由认证层保证。WebSocket 侧不是这样：
// routeRequiresWebSocketAuth 只判断 `Auth || PrivateType`，而 Manage 路由在
// service/manage/types.go 里显式 WithAuth(true)，因此 Manage 与 Auth=true 的
// ServerManage 路由都会落进同一条认证分支；authorizeAuthenticatedSubscription 又
// 一律按 types.AuthTypeUser 验签，没有按域分流。结果是普通用户 Token 能通过这些
// 路由的验签。
//
// 今天真正挡住订阅的是验签之后的三道与认证无关的机制，本测试逐条固定它们的效果：
//   - Manage：sessionsubscriptions.go 要求认证路由实现 IWebSocketUserIdentity，
//     manage.Operation[T] 没有实现，订阅被拒（客户端看到 internal server error）。
//   - Auth=true 的 ServerManage：同上。
//   - Auth=false 的 ServerManage：完全不进认证分支，最终止步于 RouteWebSocketHub
//     的服务归属校验（系统路由属于 server 服务，而 Hub 属于 shop）。
//
// 因此断言写成「跨域订阅必须失败且不留下任何订阅」，而不是断言当前这些错误文案：
// 无论隔离今后是继续依赖这些偶然护栏，还是按认证域正确分流验签，本测试都应通过；
// 只有跨域订阅真的建立起来时才会失败。
func TestWebSocketSubscribeEnforcesAuthDomain(t *testing.T) {
	t.Run("UserTokenCannotSubscribeManageRoute", testUserTokenCannotSubscribeManageRoute)
	t.Run("UserTokenCannotSubscribeServerManageRoute", testUserTokenCannotSubscribeServerManageRoute)
	t.Run("AnonymousCannotSubscribeServerManageRoute", testAnonymousCannotSubscribeServerManageRoute)
	t.Run("UserTokenCanSubscribePrivateRoute", testUserTokenCanSubscribePrivateRoute)
	t.Run("ManageTokenCannotEnterUserSession", testManageTokenCannotEnterUserSession)
	t.Run("ServerManageTokenCannotEnterUserSession", testServerManageTokenCannotEnterUserSession)
	t.Run("ManageSubscriptionRoutersStayOutsideUserIdentity", testManageSubscriptionRoutersStayOutsideUserIdentity)
}

// testUserTokenCannotSubscribeManageRoute 用一个已登录的普通用户会话订阅 Manage
// 路由。这是跨认证域水平越权的核心场景：该会话在 HTTP 侧连一次 Manage 调用都发不出去，
// 在 WebSocket 侧同样不得拿到订阅。
func testUserTokenCannotSubscribeManageRoute(t *testing.T) {
	connection := dialLogonWebSocket(t, suite.TokenFor(t, "ws-domain-user-manage", 0))
	for _, channel := range []string{manageSearchChannel, manageRemoveChannel} {
		reason := requireSubscribeRejected(t, connection, channel, map[string]interface{}{
			"SearchItem": map[string]interface{}{"page": 1, "size": 100},
		})
		t.Logf("普通用户 Token 订阅 %s 被拒，服务端文案: %s", channel, reason)
		requireNoSubscription(t, connection, channel)
	}
	requireConnectionStillUsable(t, connection)
}

// testUserTokenCannotSubscribeServerManageRoute 用同一个普通用户会话订阅 Auth=true 的
// ServerManage 路由。它与 Manage 走完全相同的认证分支，同样只接受本域 Token。
func testUserTokenCannotSubscribeServerManageRoute(t *testing.T) {
	connection := dialLogonWebSocket(t, suite.TokenFor(t, "ws-domain-user-servermanage", 0))
	reason := requireSubscribeRejected(t, connection, serverManageAuthChannel, map[string]interface{}{})
	t.Logf("普通用户 Token 订阅 %s 被拒，服务端文案: %s", serverManageAuthChannel, reason)
	requireNoSubscription(t, connection, serverManageAuthChannel)
}

// testAnonymousCannotSubscribeServerManageRoute 用完全未认证的会话订阅 Auth=false 的
// ServerManage 路由。这类路由不满足 routeRequiresWebSocketAuth，`sub` 路径上没有任何
// 认证检查，因此必须由别处保证它不会真的建立订阅。data 里给出合法 Window，避免测试
// 被路由自身的业务校验提前挡住而失去意义。
func testAnonymousCannotSubscribeServerManageRoute(t *testing.T) {
	connection := dialUnauthenticatedWebSocket(t)
	reason := requireSubscribeRejected(t, connection, serverManageOpenChannel, map[string]interface{}{
		"Window": "15s",
	})
	t.Logf("未认证会话订阅 %s 被拒，服务端文案: %s", serverManageOpenChannel, reason)
	requireNoSubscription(t, connection, serverManageOpenChannel)
}

// testUserTokenCanSubscribePrivateRoute 是对照组：同样的登录动作订阅本域的 Private
// 路由必须成功。没有它，上面几条「被拒绝」无法排除是测试写法本身有问题。
func testUserTokenCanSubscribePrivateRoute(t *testing.T) {
	connection := dialLogonWebSocket(t, suite.TokenFor(t, "ws-domain-user-private", 0))
	suite.WriteWebSocket(t, connection, "sub", privateSubscribeChannel, map[string]interface{}{})
	subscribed := suite.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "sub", subscribed.Event, string(subscribed.Data))
	require.Equal(t, privateSubscribeChannel, subscribed.Channel)

	suite.WriteWebSocket(t, connection, "get", privateSubscribeChannel, nil)
	current := suite.ReadWebSocket(t, connection, 3*time.Second)
	var subscriptions map[string]interface{}
	require.NoError(t, json.Unmarshal(current.Data, &subscriptions), string(current.Data))
	require.NotEmpty(t, subscriptions, "本域 Private 订阅必须真的建立，否则跨域用例的阴性结果没有意义")
}

// testManageTokenCannotEnterUserSession 断言 Manage Token 不能反向进入 WebSocket
// 会话身份。WebSocket 只面向最终外部用户，logon 只接受用户域 Token。
func testManageTokenCannotEnterUserSession(t *testing.T) {
	assertTokenRejectedByLogon(t, suite.TokenFor(t, "ws-domain-manage-token", 1))
}

// testServerManageTokenCannotEnterUserSession 对 ServerManage Token 做同样的断言。
func testServerManageTokenCannotEnterUserSession(t *testing.T) {
	assertTokenRejectedByLogon(t, suite.TokenFor(t, "ws-domain-server-token", 2))
}

// assertTokenRejectedByLogon 要求非用户域 Token 在 logon 阶段就被认证层拒绝，
// 并且随后的 Private 订阅同样以认证失败结束——会话不得留下任何可用身份。
func assertTokenRejectedByLogon(t *testing.T, token string) {
	t.Helper()
	connection := dialUnauthenticatedWebSocket(t)
	suite.WriteWebSocket(t, connection, "sub", "logon", map[string]string{"token": token})
	logon := suite.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "error", logon.Event, string(logon.Data))
	require.Contains(t, string(logon.Data), authenticationFailedText,
		"非用户域 Token 必须在 WebSocket 认证层被拒绝")

	reason := requireSubscribeRejected(t, connection, privateSubscribeChannel, map[string]interface{}{})
	require.Contains(t, reason, authenticationFailedText,
		"logon 失败后会话不得残留可用身份")
	requireNoSubscription(t, connection, privateSubscribeChannel)
}

// testManageSubscriptionRoutersStayOutsideUserIdentity 是上面几条集成断言的结构化补充。
//
// 今天挡住「普通用户 Token 订阅 Manage 路由」的并不是认证层，而是
// sessionsubscriptions.go 对 IWebSocketUserIdentity 的要求：manage.Operation[T] 没有
// 实现该接口，于是在验签通过之后被 fail closed。这个护栏是偶然的——只要有人给 Manage
// 操作补上 SetUserID/GetUserID（例如为了在管理端做推送），越权就会被捅穿。
//
// 本用例把这个隐式前提变成显式契约：Manage 路由确实处在「需要 WebSocket 认证」的分支
// （Auth=true），其订阅实例可以被正常创建，但不得实现用户身份接口。哪天需要让 Manage
// 操作实现 IWebSocketUserIdentity，这条断言会先失败，提醒必须同时按认证域分流验签
// （对齐 HTTP 侧 resolveRouteAuthPolicy）或在 WebSocket 层显式拒绝管理域路由。
func testManageSubscriptionRoutersStayOutsideUserIdentity(t *testing.T) {
	info := shopmanage.NewProductManage().Search.RouterInfo()
	require.True(t, info.GetAuth(), "Manage 路由声明 Auth=true，会落进 WebSocket 的认证分支")
	require.Equal(t, servertypes.ManageType, info.GetPathType())

	subscription, err := info.ParseSubscription(map[string]interface{}{})
	require.NoError(t, err, "订阅实例本身可以被创建，拒绝发生在其后的身份检查")
	require.NotNil(t, subscription)
	defer info.ReleaseSubscription(subscription)

	_, hasUserIdentity := subscription.(servertypes.IWebSocketUserIdentity)
	require.False(t, hasUserIdentity,
		"Manage 订阅实例一旦实现 IWebSocketUserIdentity，普通用户 Token 就能订阅 Manage 路由；"+
			"要加这个接口，必须先让 WebSocket 按认证域验签或显式拒绝管理域路由")
}

// dialLogonWebSocket 建立连接并用给定 Token 完成 logon，要求登录成功。
func dialLogonWebSocket(t *testing.T, token string) *websocket.Conn {
	t.Helper()
	connection := dialUnauthenticatedWebSocket(t)
	suite.WriteWebSocket(t, connection, "sub", "logon", map[string]string{"token": token})
	logon := suite.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "success", logon.Event, string(logon.Data))
	require.Equal(t, "logon", logon.Channel)
	return connection
}

// requireSubscribeRejected 发送一次 sub 并要求服务端以 error 应答，返回错误文案供日志
// 记录。返回 sub 应答即视为订阅成立，是本组用例要防的失败。
func requireSubscribeRejected(t *testing.T, connection *websocket.Conn, channel string, data interface{}) string {
	t.Helper()
	suite.WriteWebSocket(t, connection, "sub", channel, data)
	message := suite.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "error", message.Event,
		"跨认证域的 sub 不得建立订阅，channel=%s data=%s", channel, message.Data)
	require.Equal(t, channel, message.Channel)
	var reason string
	require.NoError(t, json.Unmarshal(message.Data, &reason), string(message.Data))
	return reason
}

// requireNoSubscription 用 get 事件确认被拒绝的 channel 上没有残留订阅，
// 排除「订阅其实已注册、只是应答了错误」这种更隐蔽的失败形态。
func requireNoSubscription(t *testing.T, connection *websocket.Conn, channel string) {
	t.Helper()
	suite.WriteWebSocket(t, connection, "get", channel, nil)
	message := suite.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "get", message.Event, string(message.Data))
	var subscriptions map[string]interface{}
	require.NoError(t, json.Unmarshal(message.Data, &subscriptions), string(message.Data))
	require.Empty(t, subscriptions, "被拒绝的 channel 不得残留订阅: %s", channel)
}
