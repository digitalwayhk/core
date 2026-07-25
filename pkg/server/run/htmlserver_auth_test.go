package run

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/require"
)

var htmlAuthProxyTestRoutes = []struct {
	external string
	internal string
	method   string
}{
	{"/api/servermanage/testtoken", "/api/servermanage/testtoken", http.MethodGet},
	{"/api/casdoor", "/api/casdoor", http.MethodGet},
	{"/callback", "/api/casdoor/callback", http.MethodGet},
	{"/api/refresh", "/api/refresh", http.MethodPost},
}

func htmlAuthAuthorityContext(t *testing.T, omit string) *router.ServiceContext {
	t.Helper()
	ctx := manageAuthorityContext(t, "auth-authority", true)
	for _, route := range htmlAuthProxyTestRoutes {
		if route.internal == omit {
			continue
		}
		api := &manageAuthorityTestRouter{}
		info := &types.RouterInfo{
			ID: utils.HashCode64(route.internal), Path: route.internal, Method: route.method,
			ServiceName: ctx.Service.Name, PathType: types.ServerManagerType,
			InstanceName: "AuthProxy-" + strings.ReplaceAll(route.internal, "/", "-"),
			StructName:   "manageAuthorityTestRouter",
		}
		api.info = info
		info.SetInstance(api)
		ctx.Router.AddServerRouters(api)
	}
	return ctx
}

func TestHTMLServerAuthProxyUsesCanonicalPaths(t *testing.T) {
	ctx := htmlAuthAuthorityContext(t, "")
	html := NewHTMLServer(0)
	html.SetManageAuthAuthority(&manageAuthAuthority{
		name: ctx.Service.Name, context: ctx, router: ctx.Router,
	})
	require.NoError(t, html.Prepare())
	require.NotNil(t, html.Handler())

	for _, route := range htmlAuthProxyTestRoutes {
		request := httptest.NewRequest(route.method, route.external, nil)
		request.RemoteAddr = "127.0.0.1:9"
		recorder := httptest.NewRecorder()
		html.Handler().ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code, route.external)
	}
}

func TestHTMLServerAuthProxyFailsClosedWhenRequiredRouteMissing(t *testing.T) {
	ctx := htmlAuthAuthorityContext(t, "/api/refresh")
	html := NewHTMLServer(0)
	html.SetManageAuthAuthority(&manageAuthAuthority{
		name: ctx.Service.Name, context: ctx, router: ctx.Router,
	})
	err := html.Prepare()
	require.ErrorContains(t, err, "/api/refresh")
	require.Nil(t, html.Handler())
}
