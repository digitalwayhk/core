package casdoorauthlifecycle_test

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestCasdoorWebhookClosesOldWebSocketAndAllowsRelogin(t *testing.T) {
	app := startLifecycleApp(t)
	pair := app.callback(t, string(types.AuthTypeUser), "websocket-user")
	connection := connectWebSocket(t, app, pair.AccessToken)
	channel := "/api/" + app.name + "/private"
	require.NoError(t, connection.WriteJSON(map[string]interface{}{"event": "sub", "channel": channel, "data": map[string]interface{}{}}))
	subscribed := readWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "sub", subscribed.Event)

	app.webhook(t, string(types.AuthTypeUser), "logout", "websocket-user", true)
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, _, err := connection.ReadMessage()
	require.Error(t, err, "撤销后旧 WebSocket 必须关闭")
	_ = connection.Close()

	app.webhook(t, string(types.AuthTypeUser), "login", "websocket-user", false)
	nextPair := app.callback(t, string(types.AuthTypeUser), "websocket-user")
	nextConnection := connectWebSocket(t, app, nextPair.AccessToken)
	defer nextConnection.Close()
	require.NoError(t, nextConnection.WriteJSON(map[string]interface{}{"event": "sub", "channel": channel, "data": map[string]interface{}{}}))
	require.Equal(t, "sub", readWebSocket(t, nextConnection, 3*time.Second).Event)
}
