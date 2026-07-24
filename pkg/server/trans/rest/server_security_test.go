// 本文件验证 REST 启动选项、安全响应头和最内层认证拒绝边界。
package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestRestRunOptionsDisabledCors(t *testing.T) {
	opts, err := restRunOptions(false, nil)

	require.NoError(t, err)
	require.Empty(t, opts)
}

func TestRestRunOptionsRejectsMissingOrigins(t *testing.T) {
	for _, origins := range [][]string{nil, {}, {"", "  "}} {
		_, err := restRunOptions(true, origins)
		require.ErrorContains(t, err, "CORS origin")
	}
}

func TestNormalizeCorsOriginsPreservesExplicitOrigins(t *testing.T) {
	origins := normalizeCorsOrigins([]string{" https://admin.example.com ", "", "*"})

	require.Equal(t, []string{"https://admin.example.com", "*"}, origins)
}

func TestSecurityHeaders(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "no-referrer", recorder.Header().Get("Referrer-Policy"))
	require.Equal(t, "DENY", recorder.Header().Get("X-Frame-Options"))
}

func TestSecurityHeadersPreserveExistingValues(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Referrer-Policy", "same-origin")

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, "same-origin", recorder.Header().Get("Referrer-Policy"))
}

func TestRouteHandlerRejectsNilAuthenticatedRequest(t *testing.T) {
	api := &nilRequestTestRouter{info: &types.RouterInfo{
		Path:        "/private",
		Method:      http.MethodGet,
		Auth:        true,
		PathType:    types.PrivateType,
		ServiceName: "nil-request-test",
	}}
	service := &nilRequestTestService{name: "nil-request-test", api: api}
	context := &router.ServiceContext{
		Config:  config.NewServiceDefaultConfig(service.name, 18082),
		Service: &types.Service{Name: service.name, Routers: []types.IRouter{api}},
	}
	context.Router = router.NewServiceRouter(context, service)
	handler := RouteHandler(context.Router)
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.RemoteAddr = "198.51.100.10:4321"
	recorder := httptest.NewRecorder()

	require.NotPanics(t, func() { handler.ServeHTTP(recorder, req) })
	require.Equal(t, StatusUnauthorized, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "verified access identity missing")
	require.NotContains(t, recorder.Body.String(), "internal server error")
}

type nilRequestTestService struct {
	name string
	api  types.IRouter
}

func (s *nilRequestTestService) ServiceName() string      { return s.name }
func (s *nilRequestTestService) Routers() []types.IRouter { return []types.IRouter{s.api} }

type nilRequestTestRouter struct {
	info *types.RouterInfo
}

func (r *nilRequestTestRouter) Parse(types.IRequest) error             { return nil }
func (r *nilRequestTestRouter) Validation(types.IRequest) error        { return nil }
func (r *nilRequestTestRouter) Do(types.IRequest) (interface{}, error) { return nil, nil }
func (r *nilRequestTestRouter) RouterInfo() *types.RouterInfo          { return r.info }

func newExecutableRouteHandlerTestContext(
	name, path string,
	pathType types.ApiType,
	requiresAuth bool,
) *router.ServiceContext {
	info := &types.RouterInfo{
		Path: path, Method: http.MethodGet, Auth: requiresAuth,
		PathType: pathType, ServiceName: name,
	}
	api := &nilRequestTestRouter{info: info}
	info.SetInstance(api)
	service := &nilRequestTestService{name: name, api: api}
	sc := &router.ServiceContext{
		Config:  config.NewServiceDefaultConfig(name, 18083),
		Service: &types.Service{Name: name, Routers: []types.IRouter{api}},
	}
	sc.Config.Auth.AccessSecret = "user-access-secret"
	sc.Config.ManageAuth.AccessSecret = "manage-access-secret"
	sc.Router = router.NewServiceRouter(sc, service)
	return sc
}

func TestResolveRouteAuthPolicyUsesServerManageCredentials(t *testing.T) {
	sc := newExecutableRouteHandlerTestContext(
		"server-manage-route-test", "/api/internal/openapi", types.ServerManagerType, true,
	)
	sc.Config.ServerManageAuth.AccessSecret = "server-manage-access-secret"
	sc.Router.AddServerRouters(sc.Service.Routers...)

	auth, authType := resolveRouteAuthPolicy(sc.Router, "/api/internal/openapi")

	require.Equal(t, "server-manage-access-secret", auth.AccessSecret)
	require.Equal(t, types.AuthTypeServerManage, authType)
}

func setRequestPath(request *http.Request, path string) {
	request.URL.Path = path
	request.RequestURI = path
}

func TestRouteHandlerAllowsPublicRequestWithoutIdentity(t *testing.T) {
	sc := newExecutableRouteHandlerTestContext(
		"public-route-test", "/public", types.PublicType, false,
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/public", nil)
	request.RemoteAddr = "198.51.100.10:4321"

	RouteHandler(sc.Router).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestRouteHandlerAllowsVerifiedInternalJWTIdentity(t *testing.T) {
	const path = "/private"
	sc := newExecutableRouteHandlerTestContext(
		"verified-user-route-test", path, types.PrivateType, true,
	)
	request := authenticatedRequest(t, sc.Config.Auth.AccessSecret, types.AuthIdentity{
		UID: "user-1", Username: "用户一", AuthType: types.AuthTypeUser,
	})
	setRequestPath(request, path)
	recorder := httptest.NewRecorder()
	handler := internalJWTAuthorize(
		sc.Config.Auth.AccessSecret,
		types.AuthTypeUser,
		RouteHandler(sc.Router),
	)

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestRouteHandlerRejectsVerifiedUserIdentityOnManageRoute(t *testing.T) {
	const path = "/manage"
	sc := newExecutableRouteHandlerTestContext(
		"manage-domain-route-test", path, types.ManageType, true,
	)
	request := authenticatedRequest(t, sc.Config.Auth.AccessSecret, types.AuthIdentity{
		UID: "user-1", Username: "用户一", AuthType: types.AuthTypeUser,
	})
	setRequestPath(request, path)
	recorder := httptest.NewRecorder()
	handler := internalJWTAuthorize(
		sc.Config.Auth.AccessSecret,
		types.AuthTypeUser,
		RouteHandler(sc.Router),
	)

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}
