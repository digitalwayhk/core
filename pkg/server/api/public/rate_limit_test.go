// 本文件验证系统 Public API 的限流策略，以及内部 OpenAPI 的路由和响应安全边界。
package public

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestSystemPublicRateLimitPolicies(t *testing.T) {
	tests := []struct {
		name   string
		router types.IRouter
		rate   float64
		burst  int
	}{
		{name: "callback", router: &Callback{}, rate: 5, burst: 10},
		{name: "casdoor-webhook", router: &CasdoorWebhook{}, rate: 5, burst: 10},
		{name: "refresh", router: &Refresh{}, rate: 5, burst: 10},
		{name: "health", router: &Health{}, rate: 20, burst: 40},
		{name: "casdoor", router: &Casdoor{}, rate: 10, burst: 20},
		{name: "get-menu", router: &GetMenu{}, rate: 10, burst: 20},
		{name: "query-config", router: &QueryConfig{}, rate: 10, burst: 20},
		{name: "query-routers", router: &QueryRouters{}, rate: 10, burst: 20},
		{name: "observe", router: &Observe{}, rate: 10, burst: 20},
		{name: "notify", router: &Notify{}, rate: 10, burst: 20},
		{name: "attach", router: &Attach{}, rate: 10, burst: 20},
		{name: "ip-white-list", router: &IpWhiteList{}, rate: 10, burst: 20},
		{name: "query-service", router: &QueryService{}, rate: 10, burst: 20},
		{name: "internal-openapi", router: &InternalOpenAPI{}, rate: 10, burst: 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := tt.router.RouterInfo().GetExternalRateLimit()
			require.NotNil(t, policy)
			require.Equal(t, tt.rate, policy.Rate)
			require.Equal(t, tt.burst, policy.Burst)
		})
	}
}

func TestInternalOpenAPIRouteUsesServerManageAuthDomain(t *testing.T) {
	info := (&InternalOpenAPI{}).RouterInfo()

	require.Equal(t, "/api/internal/openapi", info.GetPath())
	require.Equal(t, types.ServerManagerType, info.GetPathType())
	require.True(t, info.GetAuth())
	require.NotNil(t, info.ResponseHandlerFunc)
}

func TestInternalOpenAPIResponseIsNotCacheableAndUnwrapped(t *testing.T) {
	recorder := httptest.NewRecorder()
	response := (&router.InitRequest{}).NewResponse(map[string]string{"openapi": "3.0.1"}, nil)

	internalOpenAPIResponse(recorder, httptest.NewRequest(http.MethodGet, "/api/internal/openapi", nil), response)

	require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	require.JSONEq(t, `{"openapi":"3.0.1"}`, recorder.Body.String())
}

func TestInternalOpenAPIResponseMapsFailureToPublicErrorContract(t *testing.T) {
	recorder := httptest.NewRecorder()
	response := (&router.InitRequest{}).NewResponse(nil, errors.New("sensitive service lookup detail"))

	internalOpenAPIResponse(recorder, httptest.NewRequest(http.MethodGet, "/api/internal/openapi", nil), response)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "internal server error")
	require.NotContains(t, recorder.Body.String(), "sensitive service lookup detail")
}

func TestInternalOpenAPIRejectsUnknownServiceFilter(t *testing.T) {
	_, err := selectOpenAPIServiceRouters("service-that-does-not-exist")
	require.ErrorContains(t, err, "未找到指定服务")
}

func TestTestTokenHasNoRateLimitPolicy(t *testing.T) {
	require.Nil(t, (&TestToken{}).RouterInfo().GetExternalRateLimit())
}

func TestIpWhiteListRestoresServerArgsAccessControl(t *testing.T) {
	request := &publicRateLimitRequest{clientIP: "198.51.100.10", serviceName: "missing-service"}
	require.Error(t, (&IpWhiteList{}).Validation(request))
}

type publicRateLimitRequest struct {
	clientIP    string
	serviceName string
}

func (*publicRateLimitRequest) GetTraceId() string        { return "trace" }
func (*publicRateLimitRequest) GetUser() (string, string) { return "", "" }
func (r *publicRateLimitRequest) GetClientIP() string     { return r.clientIP }
func (*publicRateLimitRequest) NewID() uint               { return 1 }
func (*publicRateLimitRequest) Authorized() bool          { return false }
func (*publicRateLimitRequest) CallService(types.IRouter, ...func(types.IResponse)) (types.IResponse, error) {
	return nil, nil
}
func (*publicRateLimitRequest) CallTargetService(types.IRouter, *types.TargetInfo, ...func(types.IResponse)) (types.IResponse, error) {
	return nil, nil
}
func (*publicRateLimitRequest) GetValue(string) string                         { return "" }
func (*publicRateLimitRequest) Bind(interface{}) error                         { return nil }
func (*publicRateLimitRequest) GoZeroBind(interface{}) error                   { return nil }
func (*publicRateLimitRequest) NewResponse(interface{}, error) types.IResponse { return nil }
func (*publicRateLimitRequest) GetPath() string                                { return "/api/servermanage/ipwhitelist" }
func (*publicRateLimitRequest) GetClaims(string) interface{}                   { return nil }
func (r *publicRateLimitRequest) ServiceName() string                          { return r.serviceName }
func (*publicRateLimitRequest) GetServerInfo() *types.TargetInfo               { return nil }
func (*publicRateLimitRequest) GetTargetServerInfo(string) *types.TargetInfo   { return nil }
