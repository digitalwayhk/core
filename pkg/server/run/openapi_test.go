// 本文件验证公开 OpenAPI 文档的路由范围、结构有效性与空服务边界。
package run

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

type openAPITestRouter struct {
	info *types.RouterInfo
}

func (*openAPITestRouter) Parse(types.IRequest) error      { return nil }
func (*openAPITestRouter) Validation(types.IRequest) error { return nil }
func (*openAPITestRouter) Do(types.IRequest) (interface{}, error) {
	return map[string]string{"status": "ok"}, nil
}
func (r *openAPITestRouter) RouterInfo() *types.RouterInfo { return r.info }

type openAPITestService struct {
	routers []types.IRouter
}

type openAPIHMACProvider struct{}

func (*openAPIHMACProvider) AuthenticateHMAC(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error) {
	return nil, nil
}

func (*openAPITestService) ServiceName() string        { return "openapi-test" }
func (s *openAPITestService) Routers() []types.IRouter { return s.routers }

func TestGetOpenAPIWithoutServicesReturnsEmptyDocument(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://compat.example/api/openapi", nil)
	require.NoError(t, err)

	doc, ok := GetOpenApi(req).(*openapi3.T)
	require.True(t, ok)
	require.NotNil(t, doc.Paths)
	require.Zero(t, doc.Paths.Len())
	require.Empty(t, doc.Servers)
	require.Equal(t, "Bearer token authentication", doc.Components.SecuritySchemes["Bearer"].Value.Description)
	require.NoError(t, doc.Validate(context.Background()))
}

func TestGetOpenAPIFiltersInternalOnlyPublicRoutes(t *testing.T) {
	serviceRouter := newOpenAPITestServiceRouter(
		newOpenAPITestRoute("/api/openapi-test/catalog", types.PublicType),
		newOpenAPITestRoute("/api/openapi-test/orders", types.PrivateType),
		newOpenAPITestRoute("/api/openapi-test/internal-stock", types.PublicType, "shop-order"),
	)
	req := newOpenAPITestRequest(t)

	doc, ok := GetOpenApi(req, serviceRouter).(*openapi3.T)
	require.True(t, ok)
	require.NotNil(t, doc.Paths.Value("/api/openapi-test/catalog"))
	require.NotNil(t, doc.Paths.Value("/api/openapi-test/orders"))
	require.Nil(t, doc.Paths.Value("/api/openapi-test/internal-stock"))
}

func TestGetInternalOpenAPIIncludesInternalCallerMetadata(t *testing.T) {
	serviceRouter := newOpenAPITestServiceRouter(
		newOpenAPITestRoute("/api/openapi-test/internal-stock", types.PublicType, "shop-order"),
	)

	doc, ok := GetInternalOpenApi(newOpenAPITestRequest(t), serviceRouter).(*openapi3.T)
	require.True(t, ok)
	operation := doc.Paths.Value("/api/openapi-test/internal-stock").Get
	require.Equal(t, []string{"shop-order"}, operation.Extensions["x-internal-callers"])
}

// TestOpenAPIDescribesOptionalHMACAuthentication 验证 Agent 可从机器可读契约发现 Bearer OR HMAC、自定义 Header 与 WebSocket logon 字段。
func TestOpenAPIDescribesOptionalHMACAuthentication(t *testing.T) {
	serviceRouter := newOpenAPITestServiceRouter(
		newOpenAPITestRoute("/api/openapi-test/orders", types.PrivateType),
	)
	serviceRouter.Service.HMACAuthProvider = &openAPIHMACProvider{}
	serviceRouter.Service.SetServerOption(&types.ServerOption{IsWebSocket: true})
	serviceRouter.Service.Config.HMACAuth.AccessKeyHeader = "X-Test-Key"
	serviceRouter.Service.Config.HMACAuth.TimestampHeader = "X-Test-Time"
	serviceRouter.Service.Config.HMACAuth.NonceHeader = "X-Test-Nonce"
	serviceRouter.Service.Config.HMACAuth.SignatureHeader = "X-Test-Signature"

	doc, ok := GetOpenApi(newOpenAPITestRequest(t), serviceRouter).(*openapi3.T)
	require.True(t, ok)
	operation := doc.Paths.Value("/api/openapi-test/orders").Get
	require.NotNil(t, operation.Security)
	require.Len(t, *operation.Security, 2)
	require.Contains(t, (*operation.Security)[0], "Bearer")
	require.Len(t, (*operation.Security)[1], 4)

	headerNames := make(map[string]bool)
	for schemeName := range (*operation.Security)[1] {
		scheme := doc.Components.SecuritySchemes[schemeName]
		require.NotNil(t, scheme)
		require.Equal(t, "apiKey", scheme.Value.Type)
		require.Equal(t, "header", scheme.Value.In)
		headerNames[scheme.Value.Name] = true
	}
	for _, name := range []string{"X-Test-Key", "X-Test-Time", "X-Test-Nonce", "X-Test-Signature"} {
		require.True(t, headerNames[name], name)
	}
	extension, ok := operation.Extensions["x-core-hmac-auth"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, extension["bearer_priority"])
	require.Equal(t, "provider_defined", extension["algorithm"])
	require.Equal(t, "provider_defined", extension["canonicalization"])
	require.ElementsMatch(t, []string{
		"access_key", "timestamp", "nonce", "recv_window", "method", "path",
		"raw_query", "body_sha256", "client_ip", "trace_id", "path_type",
	}, extension["available_inputs"])

	logons, ok := doc.Extensions["x-core-websocket-hmac-logon"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, logons, 1)
	require.Equal(t, "sub", logons[0]["event"])
	require.Equal(t, "logon", logons[0]["channel"])
	schema, ok := logons[0]["data_schema"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "object", schema["type"])
	require.ElementsMatch(t, []string{"apiKey", "timestamp", "nonce", "signature"}, schema["required"])
	properties, ok := schema["properties"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, map[string]interface{}{"type": "integer", "format": "int64"}, properties["timestamp"])
	require.Equal(t, map[string]interface{}{"type": "string"}, properties["recvWindow"])
}

// TestOpenAPIRestOnlyHMACDoesNotAdvertiseWebSocket 验证未启用 WebSocket 的服务不会对 Agent 过度宣告 `/ws` logon 能力。
func TestOpenAPIRestOnlyHMACDoesNotAdvertiseWebSocket(t *testing.T) {
	serviceRouter := newOpenAPITestServiceRouter(
		newOpenAPITestRoute("/api/openapi-test/orders", types.PrivateType),
	)
	serviceRouter.Service.HMACAuthProvider = &openAPIHMACProvider{}
	serviceRouter.Service.SetServerOption(&types.ServerOption{IsWebSocket: false})

	doc, ok := GetOpenApi(newOpenAPITestRequest(t), serviceRouter).(*openapi3.T)
	require.True(t, ok)
	require.Contains(t, doc.Paths.Value("/api/openapi-test/orders").Get.Extensions, "x-core-hmac-auth")
	require.NotContains(t, doc.Extensions, "x-core-websocket-hmac-logon")
}

func newOpenAPITestRoute(path string, pathType types.ApiType, internalCallers ...string) types.IRouter {
	api := &openAPITestRouter{}
	api.info = &types.RouterInfo{
		Path: path, Method: http.MethodGet, PathType: pathType,
		ServiceName: "openapi-test", StructName: "openAPITestRouter",
		InternalCallers: internalCallers,
	}
	api.info.SetInstance(api)
	return api
}

func newOpenAPITestServiceRouter(routers ...types.IRouter) *router.ServiceRouter {
	service := &openAPITestService{routers: routers}
	sc := &router.ServiceContext{
		Config:  config.NewServiceDefaultConfig(service.ServiceName(), 18080),
		Service: &types.Service{Name: service.ServiceName(), Routers: routers, Instance: service},
	}
	sc.Router = router.NewServiceRouter(sc, service)
	return sc.Router
}

func newOpenAPITestRequest(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://compat.example/api/openapi", nil)
	require.NoError(t, err)
	return req
}
