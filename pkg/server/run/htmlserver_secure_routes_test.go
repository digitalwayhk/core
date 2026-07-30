package run

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestHTMLServerManageCanonicalPathRequiresJWT(t *testing.T) {
	htmlAuthReset()
	const secret = "shared-manage-access-secret-value-xx"
	sc := newHTMLManageServiceContext(t, "performanceshop", secret)
	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(sc.Router)
	require.NoError(t, htmls.Prepare())
	handler := htmls.Handler()
	path := "/api/manage/performanceshop/item/search"

	// 无 token
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	require.Equal(t, int32(0), htmlAuthCount("performanceshop:"+path))

	// 坏 token
	req = httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())

	// 有效 Manage token
	tok := issueManageToken(t, secret, "admin")
	req = httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, htmlAuthCount("performanceshop:"+path), int32(0))

	// User token 不得访问 Manage
	userTok := issueUserToken(t, "user-access-secret-for-test-xx", "buyer")
	// 给 service 配不同的 Auth secret，Manage 仍用 manage secret
	req = httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+userTok)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())

	// 兼容后缀路径同样需要 JWT
	compat := path + "/performanceshop"
	req = httptest.NewRequest(http.MethodPost, compat, strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())

	req = httptest.NewRequest(http.MethodPost, compat, strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// 错误方法
	req = httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestHTMLServerManageRoutesUseConfiguredAuthority(t *testing.T) {
	htmlAuthReset()
	const (
		serverSecret = "system-server-manage-secret-value"
		shopSecret   = "shop-own-manage-secret-value-xx"
	)
	system := newHTMLAuthServiceContext(t, "server", true)
	system.Config.ManageAuth.AccessSecret = serverSecret
	shop := newHTMLManageServiceContext(t, "shop", shopSecret)

	htmls := NewHTMLServer(0)
	htmls.SetManageAuthAuthority(&manageAuthAuthority{
		name:    "server",
		context: system,
		router:  system.Router,
	})
	htmls.AddServiceRouter(system.Router)
	htmls.AddServiceRouter(shop.Router)
	require.NoError(t, htmls.Prepare())

	path := "/api/manage/shop/item/search"
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issueManageToken(t, shopSecret, "shop-admin"))
	rec := httptest.NewRecorder()
	htmls.Handler().ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())

	req = httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issueManageToken(t, serverSecret, "developer"))
	rec = httptest.NewRecorder()
	htmls.Handler().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, htmlAuthCount("shop:"+path), int32(0))
}

func TestHTMLServerServerManageRoutesUseConfiguredManageAuthority(t *testing.T) {
	htmlAuthReset()
	const authoritySecret = "authority-manage-access-secret-value"
	authority := newHTMLAuthServiceContext(t, "orders", true)
	authority.Config.ManageAuth.AccessSecret = authoritySecret
	authority.Config.ManageAuth.RefreshSecret = authoritySecret + "-refresh"

	system := newHTMLAuthenticatedServerManagerRouteContext(
		t,
		"server",
		"/api/servermanage/getmenu",
		http.MethodGet,
	)
	system.Config.ServerManageAuth.AccessSecret = "independent-server-manage-secret"

	htmls := NewHTMLServer(0)
	htmls.SetManageAuthAuthority(&manageAuthAuthority{
		name: "orders", context: authority, router: authority.Router,
	})
	htmls.AddServiceRouter(system.Router)
	htmls.AddServiceRouter(authority.Router)
	require.NoError(t, htmls.Prepare())

	req := httptest.NewRequest(http.MethodGet, "/api/servermanage/getmenu", nil)
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Authorization", "Bearer "+issueManageToken(t, authoritySecret, "developer"))
	rec := httptest.NewRecorder()
	htmls.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, htmlAuthCount("server:/api/servermanage/getmenu"), int32(0))
}

func TestHTMLServerGetMenuCanonicalWithoutSuffix(t *testing.T) {
	htmlAuthReset()
	sc := newHTMLServerManagePublicContext(t, "server")
	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(sc.Router)
	require.NoError(t, htmls.Prepare())
	handler := htmls.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/servermanage/getmenu", nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, htmlAuthCount("server:/api/servermanage/getmenu"), int32(0))
}

func TestHTMLServerGetMenuCanonicalPrefersServerRegardlessOfRegistrationOrder(t *testing.T) {
	// 业务服务先注册时仍应选择 system server，不得 first-wins。
	htmlAuthReset()
	shop := newHTMLServerManagePublicContext(t, "shop-alpha")
	server := newHTMLServerManagePublicContext(t, "server")

	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(shop.Router) // 故意先加业务
	htmls.AddServiceRouter(server.Router)
	require.NoError(t, htmls.Prepare())

	req := httptest.NewRequest(http.MethodGet, "/api/servermanage/getmenu", nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	htmls.Handler().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, htmlAuthCount("server:/api/servermanage/getmenu"), int32(0))
	require.Equal(t, int32(0), htmlAuthCount("shop-alpha:/api/servermanage/getmenu"))

	// 兼容后缀仍可命中业务副本
	htmlAuthReset()
	req = httptest.NewRequest(http.MethodGet, "/api/servermanage/getmenu/shop-alpha", nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec = httptest.NewRecorder()
	htmls.Handler().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, htmlAuthCount("shop-alpha:/api/servermanage/getmenu"), int32(0))
}

func TestHTMLServerGetMenuCanonicalFallsBackToLexicographicWithoutServer(t *testing.T) {
	// 无 server、无 authority 时：按服务名字典序最小（alpha < zebra）。
	// authority 优先级由 TestSelectGetMenuCanonicalOwner 单测覆盖（避免 Prepare 要求完整 auth 代理）。
	htmlAuthReset()
	zebra := newHTMLServerManagePublicContext(t, "zebra")
	alpha := newHTMLServerManagePublicContext(t, "alpha")

	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(zebra.Router) // 先注册字典序更大的
	htmls.AddServiceRouter(alpha.Router)
	require.NoError(t, htmls.Prepare())
	req := httptest.NewRequest(http.MethodGet, "/api/servermanage/getmenu", nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	htmls.Handler().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Greater(t, htmlAuthCount("alpha:/api/servermanage/getmenu"), int32(0))
	require.Equal(t, int32(0), htmlAuthCount("zebra:/api/servermanage/getmenu"))
}

func TestHTMLServerServerManagerNonGetMenuOnlyCompatPath(t *testing.T) {
	// queryconfig 等不得 first-wins 暴露到规范路径；仅 /{service} 兼容。
	htmlAuthReset()
	a := newHTMLServerManagerRouteContext(t, "svc-a", "/api/servermanage/queryconfig", http.MethodPost)
	b := newHTMLServerManagerRouteContext(t, "svc-b", "/api/servermanage/queryconfig", http.MethodPost)

	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(a.Router)
	htmls.AddServiceRouter(b.Router)
	require.NoError(t, htmls.Prepare())
	h := htmls.Handler()

	// 规范无后缀未挂载：GET 走 SPA 的 /api 404 分支，且不得命中任一服务 marker
	req := httptest.NewRequest(http.MethodGet, "/api/servermanage/queryconfig", nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Equal(t, int32(0), htmlAuthCount("svc-a:/api/servermanage/queryconfig"))
	require.Equal(t, int32(0), htmlAuthCount("svc-b:/api/servermanage/queryconfig"))

	// 兼容后缀分别命中
	req = httptest.NewRequest(http.MethodPost, "/api/servermanage/queryconfig/svc-a", strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, htmlAuthCount("svc-a:/api/servermanage/queryconfig"), int32(0))

	req = httptest.NewRequest(http.MethodPost, "/api/servermanage/queryconfig/svc-b", strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, htmlAuthCount("svc-b:/api/servermanage/queryconfig"), int32(0))
}

func TestHTMLServerRuntimeGraphRoutesHaveCanonicalSystemPath(t *testing.T) {
	for _, path := range []string{
		"/api/servermanage/runtimetopology",
		"/api/servermanage/runtimeservice",
	} {
		t.Run(path, func(t *testing.T) {
			htmlAuthReset()
			system := newHTMLServerManagerRouteContext(t, "server", path, http.MethodPost)

			htmls := NewHTMLServer(0)
			htmls.AddServiceRouter(system.Router)
			require.NoError(t, htmls.Prepare())

			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
			req.RemoteAddr = "127.0.0.1:9"
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			htmls.Handler().ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Greater(t, htmlAuthCount("server:"+path), int32(0))
		})
	}
}

func TestHTMLServerReservedAuthPathsNotReRegisteredAsCompat(t *testing.T) {
	// 权威代理占用的路径不得再以 /{service} 兼容路径注册任意 ServerManager。
	htmlAuthReset()
	authority := newHTMLAuthServiceContext(t, "orders", true)
	// 另一服务也声明同名 reserved ServerManager 路由
	peer := newHTMLServerManagerRouteContext(t, "shoppeer", "/api/servermanage/testtoken", http.MethodGet)

	htmls := NewHTMLServer(0)
	htmls.SetManageAuthAuthority(&manageAuthAuthority{name: "orders", context: authority, router: authority.Router})
	htmls.AddServiceRouter(authority.Router)
	htmls.AddServiceRouter(peer.Router)
	require.NoError(t, htmls.Prepare())
	h := htmls.Handler()

	// 兼容后缀（权威服务名 / 业务服务名）均不得再注册
	for _, p := range []string{
		"/api/servermanage/testtoken/orders",
		"/api/servermanage/testtoken/shoppeer",
		"/api/refresh/orders",
		"/api/casdoor/shoppeer",
	} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.RemoteAddr = "127.0.0.1:9"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusNotFound, rec.Code, "reserved auth 不得再挂兼容后缀: %s", p)
	}
}

func TestSelectGetMenuCanonicalOwner(t *testing.T) {
	mk := func(name string) getMenuCandidate {
		return getMenuCandidate{name: name, sc: &router.ServiceContext{}, info: &types.RouterInfo{}}
	}
	cands := []getMenuCandidate{mk("zebra"), mk("server"), mk("alpha")}
	require.Equal(t, "server", selectGetMenuCanonicalOwner(cands, "zebra").name)

	cands = []getMenuCandidate{mk("zebra"), mk("alpha")}
	require.Equal(t, "zebra", selectGetMenuCanonicalOwner(cands, "zebra").name)
	require.Equal(t, "alpha", selectGetMenuCanonicalOwner(cands, "").name)
	require.Equal(t, "alpha", selectGetMenuCanonicalOwner(cands, "missing").name)
}

func TestSPAFallbackServesIndexForMainNavigation(t *testing.T) {
	dist := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>spa-root</html>")},
		"app.js":     &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	handler := spaFallbackHandler(dist)

	// 深链导航
	req := httptest.NewRequest(http.MethodGet, "/main/server/directorymanage", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "spa-root")

	// 真实静态资源
	req = httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "console.log")

	// 缺失带扩展名 → 404
	req = httptest.NewRequest(http.MethodGet, "/missing.css", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)

	// /api 不由 SPA 吞
	req = httptest.NewRequest(http.MethodGet, "/api/foo", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func newHTMLManageServiceContext(t *testing.T, name, manageSecret string) *router.ServiceContext {
	t.Helper()
	path := "/api/manage/" + strings.ToLower(name) + "/item/search"
	api := &markerAuthRouter{}
	info := &types.RouterInfo{
		Path: path, Method: http.MethodPost, ServiceName: name,
		PathType: types.ManageType, InstanceName: "Manage-" + name, StructName: "markerAuthRouter",
		PackPath: "fixture/api/manage", Auth: true,
	}
	api.info = info
	info.SetInstance(api)
	service := &htmlAuthService{name: name, routes: []types.IRouter{api}}
	cfg := config.NewServiceDefaultConfig(name, 18080)
	cfg.Host = "127.0.0.1"
	cfg.ManageAuth.AccessSecret = manageSecret
	cfg.ManageAuth.RefreshSecret = manageSecret + "-refresh"
	cfg.ManageAuth.AccessExpire = 7200
	cfg.ManageAuth.RefreshExpire = 2592000
	cfg.Auth.AccessSecret = "user-access-secret-for-test-xx"
	cfg.Auth.RefreshSecret = "user-refresh-secret-for-test-xx"
	sc := &router.ServiceContext{
		Config:  cfg,
		Service: &types.Service{Name: name, Instance: service, Routers: []types.IRouter{api}},
	}
	sc.Router = router.NewServiceRouter(sc, service)
	// NewServiceRouter already AddRoutes from Service.Routers; re-add is safe if path frozen
	require.NotNil(t, sc.Router.GetRouter(path), "manage route %s", path)
	return sc
}

func newHTMLServerManagePublicContext(t *testing.T, name string) *router.ServiceContext {
	t.Helper()
	return newHTMLServerManagerRouteContext(t, name, "/api/servermanage/getmenu", http.MethodGet)
}

func newHTMLServerManagerRouteContext(t *testing.T, name, path, method string) *router.ServiceContext {
	return newHTMLServerManagerRouteContextWithAuth(t, name, path, method, false)
}

func newHTMLAuthenticatedServerManagerRouteContext(
	t *testing.T,
	name, path, method string,
) *router.ServiceContext {
	return newHTMLServerManagerRouteContextWithAuth(t, name, path, method, true)
}

func newHTMLServerManagerRouteContextWithAuth(
	t *testing.T,
	name, path, method string,
	auth bool,
) *router.ServiceContext {
	t.Helper()
	api := &markerAuthRouter{}
	info := &types.RouterInfo{
		Path: path, Method: method, ServiceName: name,
		PathType: types.ServerManagerType, InstanceName: "SM-" + name, StructName: "markerAuthRouter",
		PackPath: "fixture/api/servermanage", Auth: auth,
	}
	api.info = info
	info.SetInstance(api)
	service := &htmlAuthService{name: name}
	cfg := config.NewServiceDefaultConfig(name, 18080)
	cfg.Host = "127.0.0.1"
	sc := &router.ServiceContext{
		Config:  cfg,
		Service: &types.Service{Name: name, Instance: service},
	}
	sc.Router = router.NewServiceRouter(sc, service)
	sc.Router.AddServerRouters(api)
	require.NotNil(t, sc.Router.GetRouter(path))
	return sc
}

func issueManageToken(t *testing.T, secret, uid string) string {
	t.Helper()
	pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims:               safe.NewClaims(uid, uid),
		AuthType:             types.AuthTypeManage,
		IssuedAt:             time.Now().UTC(),
		AccessSecret:         secret,
		AccessExpireSeconds:  3600,
		RefreshSecret:        secret + "-refresh",
		RefreshExpireSeconds: 7200,
		IssueRefresh:         true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, pair.AccessToken)
	return pair.AccessToken
}

func issueUserToken(t *testing.T, secret, uid string) string {
	t.Helper()
	pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims:               safe.NewClaims(uid, uid),
		AuthType:             types.AuthTypeUser,
		IssuedAt:             time.Now().UTC(),
		AccessSecret:         secret,
		AccessExpireSeconds:  3600,
		RefreshSecret:        secret + "-refresh",
		RefreshExpireSeconds: 7200,
		IssueRefresh:         true,
	})
	require.NoError(t, err)
	return pair.AccessToken
}
