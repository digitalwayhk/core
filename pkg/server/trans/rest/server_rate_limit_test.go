package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/ratelimit"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestExternalRateLimitReturnsTyped429BeforeDownstream(t *testing.T) {
	sc, info := rateLimitTestContext(t)
	downstreamCalls := 0
	handler := securityHeaders(externalRateLimitHandler(sc, info, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downstreamCalls++
		w.WriteHeader(http.StatusNoContent)
	})))

	first := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/callback", nil)
	request.RemoteAddr = "198.51.100.10:42000"
	handler.ServeHTTP(first, request)
	require.Equal(t, http.StatusNoContent, first.Code)

	second := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/callback", nil)
	request.RemoteAddr = "198.51.100.10:42001"
	handler.ServeHTTP(second, request)
	require.Equal(t, http.StatusTooManyRequests, second.Code)
	require.Equal(t, 1, downstreamCalls, "限流必须在认证和业务 handler 前拒绝")
	require.Equal(t, "nosniff", second.Header().Get("X-Content-Type-Options"))

	var response ErrorResponse
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &response))
	require.Equal(t, types.PublicCodeRateLimited, response.Code)
	require.Equal(t, "rate limit exceeded", response.Message)
}

func TestExternalRateLimitBypassesDirectLoopback(t *testing.T) {
	sc, info := rateLimitTestContext(t)
	handler := externalRateLimitHandler(sc, info, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for i := 0; i < 10; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/callback", nil)
		request.RemoteAddr = "127.0.0.1:42000"
		handler.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusNoContent, recorder.Code)
	}
}

func TestExternalRateLimitDoesNotBypassForwardedLoopback(t *testing.T) {
	sc, info := rateLimitTestContext(t)
	handler := externalRateLimitHandler(sc, info, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for i, expected := range []int{http.StatusNoContent, http.StatusTooManyRequests} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/callback", nil)
		request.RemoteAddr = "127.0.0.1:42000"
		request.Header.Set("X-Forwarded-For", "198.51.100.10")
		handler.ServeHTTP(recorder, request)
		require.Equal(t, expected, recorder.Code, "第 %d 次转发请求", i+1)
	}
}

func TestExternalRateLimitUsesUnknownBucketWhenClientIPFailsClosed(t *testing.T) {
	sc, info := rateLimitTestContext(t)
	handler := externalRateLimitHandler(sc, info, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	first := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/callback", nil)
	request.RemoteAddr = "invalid-remote-address"
	handler.ServeHTTP(first, request)
	require.Equal(t, http.StatusNoContent, first.Code)

	second := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/callback", nil)
	request.RemoteAddr = "another-invalid-address"
	handler.ServeHTTP(second, request)
	require.Equal(t, http.StatusTooManyRequests, second.Code)
}

func rateLimitTestContext(t *testing.T) (*router.ServiceContext, *types.RouterInfo) {
	t.Helper()
	manager := ratelimit.NewManager("rate-test", time.Minute)
	t.Cleanup(manager.Close)
	sc := &router.ServiceContext{
		Config:            &config.ServerConfig{},
		Service:           &types.Service{Name: "rate-test"},
		PublicRateLimiter: manager,
	}
	info := &types.RouterInfo{Path: "/api/callback", ServiceName: "rate-test"}
	info.ConfigureExternalRateLimit(types.ExternalRateLimitPolicy{Rate: 1, Burst: 1})
	return sc, info
}
