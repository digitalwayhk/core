// 本文件验证正式 REST 服务的访问日志只记录安全元数据，不输出请求 Header、查询参数或 Body。
package rest

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
)

func TestSecureRESTLogConfigurationReplacesOnlyEnabledNativeRequestLog(t *testing.T) {
	cfg := config.NewServiceDefaultConfig("shop", 18080)
	require.True(t, cfg.Middlewares.Log)

	secured, enabled := secureRESTLogConfiguration(cfg.RestConf)

	require.True(t, enabled)
	require.False(t, secured.Middlewares.Log)
	require.True(t, cfg.Middlewares.Log, "不得修改调用方持有的服务配置")

	cfg.Middlewares.Log = false
	secured, enabled = secureRESTLogConfiguration(cfg.RestConf)
	require.False(t, enabled)
	require.False(t, secured.Middlewares.Log)
}

func TestSafeHTTPAccessLogDoesNotDumpSensitiveRequestOnServerError(t *testing.T) {
	var output bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&output))
	t.Cleanup(func() {
		logx.SetWriter(previous)
		logx.Reset()
	})

	const (
		rawToken   = "server-error-secret-token"
		rawCookie  = "server-error-secret-cookie"
		queryValue = "server-error-secret-query"
		bodyValue  = "server-error-secret-body"
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/shop/order?debug="+queryValue,
		strings.NewReader(`{"secret":"`+bodyValue+`"}`),
	)
	request.Header.Set("Authorization", "Bearer "+rawToken)
	request.Header.Set("Cookie", "session="+rawCookie)
	recorder := httptest.NewRecorder()

	safeHTTPAccessLog("shop", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	})).ServeHTTP(recorder, request)

	logged := output.String()
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, logged, "http_request_failed")
	require.Contains(t, logged, `"service":"shop"`)
	require.Contains(t, logged, `"method":"POST"`)
	require.Contains(t, logged, `"route":"/api/shop/order"`)
	require.Contains(t, logged, `"status":500`)
	require.NotContains(t, logged, rawToken)
	require.NotContains(t, logged, rawCookie)
	require.NotContains(t, logged, queryValue)
	require.NotContains(t, logged, bodyValue)
	require.NotContains(t, logged, "Authorization")
	require.NotContains(t, logged, "Cookie")
}

func TestSafeHTTPAccessLogPreservesSuccessfulResponse(t *testing.T) {
	var output bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&output))
	t.Cleanup(func() {
		logx.SetWriter(previous)
		logx.Reset()
	})

	request := httptest.NewRequest(http.MethodGet, "/api/shop/orders", nil)
	recorder := httptest.NewRecorder()

	safeHTTPAccessLog("shop", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Contains(t, output.String(), "http_request_completed")
	require.Contains(t, output.String(), `"status":204`)
}

func TestSafeHTTPAccessLogRecordsProtocolSwitch(t *testing.T) {
	var output bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&output))
	t.Cleanup(func() {
		logx.SetWriter(previous)
		logx.Reset()
	})

	request := httptest.NewRequest(http.MethodGet, "/ws", nil)
	recorder := httptest.NewRecorder()

	safeHTTPAccessLog("shop", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusSwitchingProtocols)
	})).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusSwitchingProtocols, recorder.Code)
	require.Contains(t, output.String(), `"status":101`)
}
