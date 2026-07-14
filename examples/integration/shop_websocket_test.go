package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// websocketMessage 对应框架 WebSocket 的 event/channel/data 信封。
type websocketMessage struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

// orderEvent 是用户订阅收到的订单变更消息。
type orderEvent struct {
	Action string   `json:"action"`
	Order  orderDTO `json:"order"`
}

// TestOrderWebSocketIsIsolatedByUser 验证登录、订阅以及新增和删除通知的用户隔离。
func TestOrderWebSocketIsIsolatedByUser(t *testing.T) {
	adminToken := tokenFor(t, "ws-admin", 1)
	userAToken := tokenFor(t, "ws-user-a", 0)
	userBToken := tokenFor(t, "ws-user-b", 0)
	product := addProduct(t, adminToken, "WebSocket 商品", "12.50")
	unauthenticated, _, err := websocket.DefaultDialer.Dial(suite.wsURL, nil)
	require.NoError(t, err)
	writeWS(t, unauthenticated, "sub", "/api/shop/getorders", map[string]interface{}{})
	unauthenticatedReply := readWS(t, unauthenticated, 3*time.Second)
	assert.Equal(t, "error", unauthenticatedReply.Event)
	_ = unauthenticated.Close()

	connectionA := connectAndSubscribe(t, userAToken)
	defer connectionA.Close()
	connectionB := connectAndSubscribe(t, userBToken)
	defer connectionB.Close()
	messagesB := streamWS(t, connectionB)

	created := requestJSON(t, http.MethodPost, "/api/shop/addorder", userAToken, map[string]interface{}{
		"productID": uintID(t, product.ID),
		"quantity":  3,
	})
	require.True(t, created.Success, created.ErrorMessage)
	createdEvent := readOrderEvent(t, connectionA)
	assert.Equal(t, "created", createdEvent.Action)
	assert.Equal(t, "ws-user-a", createdEvent.Order.UserID)
	assertNoOrderEvent(t, messagesB)

	deleted := requestJSON(t, http.MethodPost, "/api/shop/deleteorder", userAToken, map[string]string{"id": createdEvent.Order.ID})
	require.True(t, deleted.Success, deleted.ErrorMessage)
	deletedEvent := readOrderEvent(t, connectionA)
	assert.Equal(t, "deleted", deletedEvent.Action)
	assert.Equal(t, createdEvent.Order.ID, deletedEvent.Order.ID)
	assertNoOrderEvent(t, messagesB)
}

// connectAndSubscribe 建立 WebSocket、使用 TestToken 登录并订阅本人订单。
func connectAndSubscribe(t *testing.T, token string) *websocket.Conn {
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial(suite.wsURL, nil)
	require.NoError(t, err)
	writeWS(t, connection, "sub", "logon", map[string]string{"token": token})
	logon := readWS(t, connection, 3*time.Second)
	require.Equal(t, "success", logon.Event, string(logon.Data))
	require.Equal(t, "logon", logon.Channel)

	writeWS(t, connection, "sub", "/api/shop/getorders", map[string]interface{}{})
	subscribed := readWS(t, connection, 3*time.Second)
	require.Equal(t, "sub", subscribed.Event, string(subscribed.Data))
	require.Equal(t, "/api/shop/getorders", subscribed.Channel)
	return connection
}

// writeWS 发送符合框架协议的 WebSocket 消息。
func writeWS(t *testing.T, connection *websocket.Conn, event, channel string, data interface{}) {
	t.Helper()
	require.NoError(t, connection.WriteJSON(map[string]interface{}{
		"event":   event,
		"channel": channel,
		"data":    data,
	}))
}

// readWS 在给定时限内读取并解析一条 WebSocket 消息。
func readWS(t *testing.T, connection *websocket.Conn, timeout time.Duration) websocketMessage {
	t.Helper()
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(timeout)))
	_, data, err := connection.ReadMessage()
	require.NoError(t, err)
	var message websocketMessage
	require.NoError(t, json.Unmarshal(data, &message), string(data))
	return message
}

// readOrderEvent 跳过非订单消息并返回当前用户收到的订单变更。
func readOrderEvent(t *testing.T, connection *websocket.Conn) orderEvent {
	t.Helper()
	message := readWS(t, connection, 3*time.Second)
	require.Equal(t, "/api/shop/getorders", message.Channel)
	var event orderEvent
	require.NoError(t, json.Unmarshal(message.Data, &event), string(message.Data))
	return event
}

// assertNoOrderEvent 验证另一用户在短窗口内没有收到订单通知。
func assertNoOrderEvent(t *testing.T, messages <-chan websocketMessage) {
	t.Helper()
	select {
	case message := <-messages:
		t.Fatalf("其他用户不应收到订单事件: %+v", message)
	case <-time.After(250 * time.Millisecond):
	}
}

// streamWS 持续读取指定连接，使多次“没有消息”断言不会破坏连接状态。
func streamWS(t *testing.T, connection *websocket.Conn) <-chan websocketMessage {
	t.Helper()
	require.NoError(t, connection.SetReadDeadline(time.Time{}))
	messages := make(chan websocketMessage, 4)
	go func() {
		defer close(messages)
		for {
			_, data, err := connection.ReadMessage()
			if err != nil {
				return
			}
			var message websocketMessage
			if json.Unmarshal(data, &message) == nil {
				messages <- message
			}
		}
	}()
	return messages
}
