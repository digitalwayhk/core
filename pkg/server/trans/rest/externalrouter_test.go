package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type externalHandlerTestRouter struct {
	info *types.RouterInfo
}

func (*externalHandlerTestRouter) Parse(types.IRequest) error      { return nil }
func (*externalHandlerTestRouter) Validation(types.IRequest) error { return nil }
func (*externalHandlerTestRouter) Do(types.IRequest) (interface{}, error) {
	return map[string]string{"marker": "external-handler-executed"}, nil
}
func (r *externalHandlerTestRouter) RouterInfo() *types.RouterInfo { return r.info }

type externalHandlerTestService struct {
	name   string
	routes []types.IRouter
}

func (s *externalHandlerTestService) ServiceName() string      { return s.name }
func (s *externalHandlerTestService) Routers() []types.IRouter { return s.routes }

func externalHandlerContext(
	t *testing.T,
	pathType types.ApiType,
	auth bool,
) (*router.ServiceContext, *types.RouterInfo) {
	t.Helper()
	name := "external-" + strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	path := "/api/" + strings.ToLower(string(pathType)) + "/" + name
	api := &externalHandlerTestRouter{}
	info := &types.RouterInfo{
		Path: path, Method: http.MethodGet, ServiceName: name,
		PathType: pathType, Auth: auth, InstanceName: "External-" + name,
		StructName: "externalHandlerTestRouter",
	}
	api.info = info
	info.SetInstance(api)
	routes := []types.IRouter{api}
	if pathType == types.ServerManagerType {
		routes = nil
	}
	service := &externalHandlerTestService{name: name, routes: routes}
	cfg := config.NewServiceDefaultConfig(name, 18080)
	cfg.Auth.AccessSecret = "user-domain-secret"
	cfg.ManageAuth.AccessSecret = "manage-domain-secret"
	cfg.ServerManageAuth.AccessSecret = "server-domain-secret"
	sc := &router.ServiceContext{
		Config: cfg,
		Service: &types.Service{
			Name: name, Instance: service, Routers: service.Routers(),
		},
	}
	sc.Router = router.NewServiceRouter(sc, service)
	if pathType == types.ServerManagerType {
		sc.Router.AddServerRouters(api)
	}
	registered := sc.Router.GetRouter(path)
	require.NotNil(t, registered)
	return sc, registered
}

func externalAccessToken(t *testing.T, secret string, authType types.AuthType) string {
	t.Helper()
	now := time.Now().UTC().Add(-time.Second)
	pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims: safe.NewClaims("user-1", "Alice"),
		Identity: types.AuthIdentity{
			UID: "user-1", Username: "Alice", AuthType: authType,
		},
		AuthType: authType, IssuedAt: now,
		AccessSecret: secret, AccessExpireSeconds: 3600,
	})
	require.NoError(t, err)
	return pair.AccessToken
}

func TestExternalRouterRejectsInvalidOrUnregisteredRoute(t *testing.T) {
	sc, _ := externalHandlerContext(t, types.PublicType, false)
	_, err := NewExternalRouterHandler(nil, nil)
	require.Error(t, err)

	unregistered := &types.RouterInfo{Path: "/not/registered", Method: http.MethodGet}
	_, err = NewExternalRouterHandler(sc.Router, unregistered)
	require.ErrorContains(t, err, "未注册")
}

func TestExternalRouterPreservesMethodIPAndResponseHandler(t *testing.T) {
	sc, info := externalHandlerContext(t, types.PublicType, false)
	sc.Config.IsLoaclVisit = true
	info.ResponseHandlerFunc = func(w http.ResponseWriter, _ *http.Request, _ types.IResponse) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("custom-response"))
	}
	handler, err := NewExternalRouterHandler(sc.Router, info)
	require.NoError(t, err)

	wrongMethod := httptest.NewRecorder()
	handler.ServeHTTP(wrongMethod, httptest.NewRequest(http.MethodPost, info.GetPath(), nil))
	require.Equal(t, http.StatusMethodNotAllowed, wrongMethod.Code)
	require.Equal(t, http.MethodGet, wrongMethod.Header().Get("Allow"))

	remoteRequest := httptest.NewRequest(http.MethodGet, info.GetPath(), nil)
	remoteRequest.RemoteAddr = "203.0.113.10:9"
	remote := httptest.NewRecorder()
	handler.ServeHTTP(remote, remoteRequest)
	require.Equal(t, http.StatusForbidden, remote.Code)
	require.NotContains(t, remote.Body.String(), "external-handler-executed")
	require.NotContains(t, remote.Body.String(), "custom-response")

	localRequest := httptest.NewRequest(http.MethodGet, info.GetPath(), nil)
	localRequest.RemoteAddr = "127.0.0.1:9"
	local := httptest.NewRecorder()
	handler.ServeHTTP(local, localRequest)
	require.Equal(t, http.StatusAccepted, local.Code)
	require.Equal(t, "custom-response", local.Body.String())
	require.Equal(t, "nosniff", local.Header().Get("X-Content-Type-Options"))
}

func TestExternalRouterUsesOriginalAuthDomain(t *testing.T) {
	tests := []struct {
		name     string
		pathType types.ApiType
		authType types.AuthType
		secret   string
		wrong    string
	}{
		{"private", types.PrivateType, types.AuthTypeUser, "user-domain-secret", "manage-domain-secret"},
		{"manage", types.ManageType, types.AuthTypeManage, "manage-domain-secret", "user-domain-secret"},
		{"server manage", types.ServerManagerType, types.AuthTypeServerManage, "server-domain-secret", "user-domain-secret"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc, info := externalHandlerContext(t, tc.pathType, true)
			handler, err := NewExternalRouterHandler(sc.Router, info)
			require.NoError(t, err)

			validRequest := httptest.NewRequest(http.MethodGet, info.GetPath(), nil)
			validRequest.RemoteAddr = "127.0.0.1:9"
			validRequest.Header.Set("Authorization", "Bearer "+externalAccessToken(t, tc.secret, tc.authType))
			valid := httptest.NewRecorder()
			handler.ServeHTTP(valid, validRequest)
			require.Equal(t, http.StatusOK, valid.Code)
			require.Contains(t, valid.Body.String(), "external-handler-executed")

			wrongRequest := httptest.NewRequest(http.MethodGet, info.GetPath(), nil)
			wrongRequest.RemoteAddr = "127.0.0.1:9"
			wrongRequest.Header.Set("Authorization", "Bearer "+externalAccessToken(t, tc.wrong, tc.authType))
			wrong := httptest.NewRecorder()
			handler.ServeHTTP(wrong, wrongRequest)
			require.Equal(t, http.StatusUnauthorized, wrong.Code)
			require.NotContains(t, wrong.Body.String(), "external-handler-executed")
		})
	}
}
