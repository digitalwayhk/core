// 本文件通过真实 REST 端口验证 Casdoor 双认证域生命周期及 500 响应日志的凭据隔离。
package casdoorauthlifecycle_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
)

func TestCasdoorAuthLifecycleREST(t *testing.T) {
	app := startLifecycleApp(t)

	var logOutput bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&logOutput))
	t.Cleanup(func() {
		logx.SetWriter(previous)
		logx.Reset()
	})

	authPair := app.callback(t, string(types.AuthTypeUser), "alice")
	managePair := app.callback(t, string(types.AuthTypeManage), "manager")

	privateStatus, privateResponse := app.request(t, http.MethodGet, "/api/"+app.name+"/private", authPair.AccessToken, nil, nil)
	require.Equal(t, http.StatusOK, privateStatus, privateResponse.publicMessage())
	manageStatus, manageResponse := app.request(t, http.MethodGet, "/api/"+app.name+"/manage", managePair.AccessToken, nil, nil)
	require.Equal(t, http.StatusOK, manageStatus, manageResponse.publicMessage())

	status, _ := app.request(t, http.MethodGet, "/api/"+app.name+"/manage", authPair.AccessToken, nil, nil)
	assert.Equal(t, http.StatusUnauthorized, status)
	status, _ = app.request(t, http.MethodGet, "/api/"+app.name+"/private", managePair.AccessToken, nil, nil)
	assert.Equal(t, http.StatusUnauthorized, status)

	typedPair := app.callback(t, string(types.AuthTypeUser), "typed")
	status, response := app.request(t, http.MethodGet, "/api/"+app.name+"/private", typedPair.AccessToken, nil, nil)
	assert.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, "账户已冻结", response.publicMessage())

	internalPair := app.callback(t, string(types.AuthTypeUser), "internal")
	status, response = app.request(t, http.MethodGet, "/api/"+app.name+"/private", internalPair.AccessToken, nil, nil)
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Equal(t, "internal server error", response.publicMessage())
	assert.NotContains(t, response.publicMessage(), "authorization detail")
	assert.Contains(t, logOutput.String(), "http_request_failed")
	assert.NotContains(t, logOutput.String(), internalPair.AccessToken)
	assert.NotContains(t, logOutput.String(), "Authorization")

	app.webhook(t, string(types.AuthTypeUser), "logout", "alice", true)
	status, _ = app.request(t, http.MethodGet, "/api/"+app.name+"/private", authPair.AccessToken, nil, nil)
	assert.Equal(t, http.StatusUnauthorized, status)
	status, _ = app.request(t, http.MethodPost, "/api/refresh", "", map[string]string{"token": authPair.RefreshToken, "type": "auth"}, nil)
	assert.Equal(t, http.StatusUnauthorized, status)

	app.webhook(t, string(types.AuthTypeUser), "login", "alice", false)
	nextPair := app.callback(t, string(types.AuthTypeUser), "alice")
	status, _ = app.request(t, http.MethodGet, "/api/"+app.name+"/private", nextPair.AccessToken, nil, nil)
	assert.Equal(t, http.StatusOK, status)

	status, _ = app.request(t, http.MethodGet, "/api/"+app.name+"/public", "", nil, nil)
	assert.Equal(t, http.StatusOK, status)
}
