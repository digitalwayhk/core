// 本文件通过真实 REST、WebSocket 与本地撤销存储验证展示 UID 的完整认证生命周期。
package casdoorauthlifecycle_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type displayUIDHook struct{}

func (displayUIDHook) OnAuth(_ context.Context, args *types.AuthHookArgs) error {
	if args.AuthType != types.AuthTypeUser {
		return nil
	}
	args.Claims.AddData("platform_uid", "900000000000001")
	return args.Claims.(types.ITokenUIDMutator).SetTokenUID("1234567")
}

// TestCasdoorDisplayUIDRESTWebSocketAndLogout 验证短 ID 签发、刷新、私有请求与撤销后拒绝旧连接。
func TestCasdoorDisplayUIDRESTWebSocketAndLogout(t *testing.T) {
	app := startLifecycleApp(t, displayUIDHook{})
	pair := app.callback(t, "auth", "alice")
	verified, err := safe.ValidateAccessToken(pair.AccessToken, app.context.Config.Auth.AccessSecret, types.AuthTypeUser, time.Now())
	require.NoError(t, err)
	require.Equal(t, "1234567", verified.Claims["uid"])
	require.Equal(t, "auth-org-alice", verified.Identity.UID)
	status, response := app.request(t, http.MethodGet, "/api/"+app.name+"/private", pair.AccessToken, nil, nil)
	require.Equal(t, http.StatusOK, status, response.publicMessage())
	var data map[string]string
	require.NoError(t, json.Unmarshal(response.Data, &data))
	require.Equal(t, "auth-org-alice", data["uid"], "服务端请求身份必须保留原始认证 UID")
	status, response = app.request(t, http.MethodPost, "/api/refresh", "", map[string]string{"token": pair.RefreshToken, "type": "auth"}, nil)
	require.Equal(t, http.StatusOK, status, response.publicMessage())
	var refreshed tokenPair
	require.NoError(t, json.Unmarshal(response.Data, &refreshed))
	verified, err = safe.ValidateAccessToken(refreshed.AccessToken, app.context.Config.Auth.AccessSecret, types.AuthTypeUser, time.Now())
	require.NoError(t, err)
	require.Equal(t, "1234567", verified.Claims["uid"])
	connection := connectWebSocket(t, app, refreshed.AccessToken)
	defer connection.Close()
	require.NoError(t, connection.WriteJSON(map[string]interface{}{"event": "sub", "channel": "/api/" + app.name + "/private", "data": map[string]interface{}{}}))
	require.Equal(t, "sub", readWebSocket(t, connection, 3*time.Second).Event)
	app.webhook(t, "auth", "logout", "alice", true)
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, _, err = connection.ReadMessage()
	require.Error(t, err, "按原始 UUID 撤销后，短 ID Token 的连接必须关闭")
	status, _ = app.request(t, http.MethodGet, "/api/"+app.name+"/private", pair.AccessToken, nil, nil)
	require.Equal(t, http.StatusUnauthorized, status)
	status, _ = app.request(t, http.MethodPost, "/api/refresh", "", map[string]string{"token": pair.RefreshToken, "type": "auth"}, nil)
	require.Equal(t, http.StatusUnauthorized, status)
}
