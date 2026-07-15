package public

import (
	"testing"

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
		{name: "openapi", router: &OpenAPI{}, rate: 10, burst: 20},
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
