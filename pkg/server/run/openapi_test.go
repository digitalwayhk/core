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
