package run

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

const htmlSwaggerUserSecret = "html-swagger-user-access-secret-xx"
const htmlSwaggerManageSecret = "html-swagger-manage-access-secret-xx"

func openAPITestServiceRouter(name string, port int) *router.ServiceRouter {
	cfg := &config.ServerConfig{}
	cfg.Port = port
	return &router.ServiceRouter{
		Service: &router.ServiceContext{
			Config: cfg,
			Service: &types.Service{
				Name: name,
			},
		},
	}
}

func newHTMLBusinessServiceContext(t *testing.T, name string, publicPath, privatePath string) *router.ServiceContext {
	t.Helper()
	var routes []types.IRouter

	if publicPath != "" {
		api := &markerAuthRouter{}
		info := &types.RouterInfo{
			Path: publicPath, Method: http.MethodGet, ServiceName: name,
			PathType: types.PublicType, InstanceName: "Public-" + name, StructName: "markerAuthRouter",
			PackPath: "fixture/api/public", Auth: false,
		}
		api.info = info
		info.SetInstance(api)
		routes = append(routes, api)
	}
	if privatePath != "" {
		api := &markerAuthRouter{}
		info := &types.RouterInfo{
			Path: privatePath, Method: http.MethodGet, ServiceName: name,
			PathType: types.PrivateType, InstanceName: "Private-" + name, StructName: "markerAuthRouter",
			PackPath: "fixture/api/private", Auth: true,
		}
		api.info = info
		info.SetInstance(api)
		routes = append(routes, api)
	}

	service := &htmlAuthService{name: name, routes: routes}
	cfg := config.NewServiceDefaultConfig(name, 21001)
	cfg.Host = "127.0.0.1"
	cfg.Auth.AccessSecret = htmlSwaggerUserSecret
	cfg.Auth.RefreshSecret = htmlSwaggerUserSecret + "-refresh"
	cfg.Auth.AccessExpire = 7200
	cfg.Auth.RefreshExpire = 2592000
	cfg.ManageAuth.AccessSecret = htmlSwaggerManageSecret
	cfg.ManageAuth.RefreshSecret = htmlSwaggerManageSecret + "-refresh"
	cfg.ManageAuth.AccessExpire = 7200
	cfg.ManageAuth.RefreshExpire = 2592000
	sc := &router.ServiceContext{
		Config:  cfg,
		Service: &types.Service{Name: name, Instance: service, Routers: routes},
	}
	sc.Router = router.NewServiceRouter(sc, service)
	if publicPath != "" {
		require.NotNil(t, sc.Router.GetRouter(publicPath), "public %s", publicPath)
	}
	if privatePath != "" {
		require.NotNil(t, sc.Router.GetRouter(privatePath), "private %s", privatePath)
	}
	return sc
}

func TestHTMLServerPublicRouteSameOriginWithoutCORS(t *testing.T) {
	htmlAuthReset()
	const publicPath = "/api/demo/getproducts"
	sc := newHTMLBusinessServiceContext(t, "demo", publicPath, "/api/demo/getorder")
	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(sc.Router)
	require.NoError(t, htmls.Prepare())
	handler := htmls.Handler()
	require.NotNil(t, handler)

	req := httptest.NewRequest(http.MethodGet, publicPath, nil)
	req.Host = "[::1]:48080"
	req.RemoteAddr = "127.0.0.1:9"
	// Simulate browser same-origin Swagger Try-it-out (no Origin / CORS dance).
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"path":"`+publicPath+`"`)
	require.Greater(t, htmlAuthCount("demo:"+publicPath), int32(0))
	// Must not depend on permissive CORS for Try-it-out.
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestHTMLServerPrivateRouteAuthMatrix(t *testing.T) {
	htmlAuthReset()
	const privatePath = "/api/demo/getorder"
	sc := newHTMLBusinessServiceContext(t, "demo", "/api/demo/getproducts", privatePath)
	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(sc.Router)
	require.NoError(t, htmls.Prepare())
	handler := htmls.Handler()

	// 无 token → 401，不得执行 Do
	req := httptest.NewRequest(http.MethodGet, privatePath, nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	require.Equal(t, int32(0), htmlAuthCount("demo:"+privatePath))
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))

	// 普通用户 Bearer → 200
	userTok := issueUserToken(t, htmlSwaggerUserSecret, "buyer-1")
	req = httptest.NewRequest(http.MethodGet, privatePath, nil)
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Authorization", "Bearer "+userTok)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, htmlAuthCount("demo:"+privatePath), int32(0))

	// Manage Token 不得访问 Private → 401
	htmlAuthReset()
	manageTok := issueManageToken(t, htmlSwaggerManageSecret, "admin-1")
	req = httptest.NewRequest(http.MethodGet, privatePath, nil)
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Authorization", "Bearer "+manageTok)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	require.Equal(t, int32(0), htmlAuthCount("demo:"+privatePath))
}

func TestHTMLServerOpenAPIPreservesServicePortsAndPrivateTokenIssuer(t *testing.T) {
	htmlAuthReset()
	const userPublicPath = "/api/shop-user/profile"
	const userPrivatePath = "/api/shop-user/getorders"
	const orderPrivatePath = "/api/shop-order/getorder"
	const managePath = "/api/manage/shop-user/item/search"
	user := newHTMLBusinessServiceContext(t, "shop-user", userPublicPath, userPrivatePath)
	user.Config.Port = 8082
	order := newHTMLBusinessServiceContext(t, "shop-order", "", orderPrivatePath)
	order.Config.Port = 8083
	// Attach a manage route to ensure it stays out of OpenAPI document.
	manageAPI := &markerAuthRouter{}
	manageInfo := &types.RouterInfo{
		Path: managePath, Method: http.MethodPost, ServiceName: "shop-user",
		PathType: types.ManageType, InstanceName: "Manage-shop-user", StructName: "markerAuthRouter",
		PackPath: "fixture/api/manage", Auth: true,
	}
	manageAPI.info = manageInfo
	manageInfo.SetInstance(manageAPI)
	user.Router.AddRoutes(manageAPI)

	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(order.Router)
	htmls.AddServiceRouter(user.Router)
	require.NoError(t, htmls.Prepare())
	handler := htmls.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/openapi", nil)
	req.Host = "localhost"
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var doc openapi3.T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	require.Len(t, doc.Servers, 2, "多服务 OpenAPI 必须保留每个业务服务的连接")
	require.Equal(t, []string{"http://localhost:8083/", "http://localhost:8082/"},
		[]string{doc.Servers[0].URL, doc.Servers[1].URL})
	// Public + Private in document; Manage excluded.
	require.NotNil(t, doc.Paths.Find(userPublicPath))
	require.NotNil(t, doc.Paths.Find(userPrivatePath))
	require.NotNil(t, doc.Paths.Find(orderPrivatePath))
	require.Nil(t, doc.Paths.Find(managePath))
	require.Equal(t, "http://localhost:8082/",
		(*doc.Paths.Find(userPrivatePath).Get.Servers)[0].URL)
	require.Equal(t, "http://localhost:8083/",
		(*doc.Paths.Find(orderPrivatePath).Get.Servers)[0].URL)
	require.Contains(t,
		doc.Components.SecuritySchemes["Bearer"].Value.Description,
		"http://localhost:8082/api/servermanage/testtoken?userid=12345",
		"shop-user Private 的示例 Token 必须从 shop-user 服务签发")
	require.Contains(t,
		doc.Components.SecuritySchemes["Bearer"].Value.Description,
		"http://localhost:8083/api/servermanage/testtoken?userid=12345",
		"shop-order Private 的示例 Token 必须从 shop-order 服务签发")
}

func TestGetOpenApiBusinessServiceStillUsesServicePort(t *testing.T) {
	// 独立业务端 OpenAPI 仍保留服务端口语义（非 HTML 同源模式）。
	sr := openAPITestServiceRouter("demo", 21001)
	// Register empty maps ok — only server URL checked.
	req, err := http.NewRequest(http.MethodGet, "http://example/api/openapi", nil)
	require.NoError(t, err)
	req.Host = "[::1]:19090"
	doc, ok := GetOpenApi(req, sr).(*openapi3.T)
	require.True(t, ok)
	require.Len(t, doc.Servers, 1)
	require.Equal(t, "http://[::1]:21001/", doc.Servers[0].URL)
}

func TestHTMLServerUnknownAPIStill404(t *testing.T) {
	htmlAuthReset()
	sc := newHTMLBusinessServiceContext(t, "demo", "/api/demo/getproducts", "")
	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(sc.Router)
	require.NoError(t, htmls.Prepare())
	handler := htmls.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/demo/does-not-exist", nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestHTMLServerPublicPrivatePathConflictFailClosed(t *testing.T) {
	htmlAuthReset()
	const path = "/api/shared/getproducts"
	first := newHTMLBusinessServiceContext(t, "alpha", path, "")
	second := newHTMLBusinessServiceContext(t, "beta", path, "")

	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(first.Router)
	htmls.AddServiceRouter(second.Router)
	err := htmls.Prepare()
	require.Error(t, err)
	require.ErrorContains(t, err, path)
	require.True(t,
		strings.Contains(err.Error(), "alpha") || strings.Contains(err.Error(), "beta"),
		"conflict error should name services: %v", err)
	require.Nil(t, htmls.Handler(), "Prepare fail-closed must not leave partial handler")
}

func TestHTMLServerPublicConflictWithManageAuthProxyFailClosed(t *testing.T) {
	htmlAuthReset()
	// Public trying to claim reserved auth proxy path must fail closed.
	authority := newHTMLAuthServiceContext(t, "orders", true)
	conflict := newHTMLBusinessServiceContext(t, "evil", "/api/refresh", "")

	htmls := NewHTMLServer(0)
	htmls.SetManageAuthAuthority(&manageAuthAuthority{
		name: "orders", context: authority, router: authority.Router,
	})
	htmls.AddServiceRouter(authority.Router)
	htmls.AddServiceRouter(conflict.Router)
	err := htmls.Prepare()
	require.Error(t, err)
	require.ErrorContains(t, err, "/api/refresh")
	require.Nil(t, htmls.Handler())
}

func TestHTMLServerPublicConflictWithFixedSystemPathsFailClosed(t *testing.T) {
	htmlAuthReset()
	queryPath := qs.RouterInfo().GetPath()
	cases := []struct {
		name string
		path string
	}{
		{name: "openapi", path: "/api/openapi"},
		{name: "bootstrap", path: webBootstrapPath},
		{name: "queryservice", path: queryPath},
		{name: "swagger-subtree", path: "/swagger/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			htmlAuthReset()
			sc := newHTMLBusinessServiceContext(t, "evil-"+tc.name, tc.path, "")
			htmls := NewHTMLServer(0)
			htmls.AddServiceRouter(sc.Router)
			var err error
			require.NotPanics(t, func() {
				err = htmls.Prepare()
			})
			require.Error(t, err, "path %s must fail Prepare", tc.path)
			require.ErrorContains(t, err, tc.path)
			require.Nil(t, htmls.Handler())
		})
	}
}

func TestHTMLServerManageConflictWithFixedOpenAPIPathFailClosed(t *testing.T) {
	htmlAuthReset()
	// Manage 抢占系统固定路径须 fail-closed；不得触发 ServeMux panic。
	sc := newHTMLManageServiceContext(t, "evilmanage", "shared-manage-access-secret-value-xx")
	// 用非法系统路径覆盖：直接挂一个 Manage 路由到 /api/openapi
	api := &markerAuthRouter{}
	info := &types.RouterInfo{
		Path: "/api/openapi", Method: http.MethodPost, ServiceName: "evilmanage",
		PathType: types.ManageType, InstanceName: "Manage-openapi", StructName: "markerAuthRouter",
		PackPath: "fixture/api/manage", Auth: true,
	}
	api.info = info
	info.SetInstance(api)
	sc.Router.AddRoutes(api)

	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(sc.Router)
	var err error
	require.NotPanics(t, func() {
		err = htmls.Prepare()
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "/api/openapi")
	require.Nil(t, htmls.Handler())
}

func TestHTMLServerManageCrossServiceConflictStillCompat(t *testing.T) {
	// 既有 Manage 跨服务冲突：first-wins 规范路径 + 后到者仅兼容后缀，不得因统一 mount helper 而 fail-closed。
	htmlAuthReset()
	const secret = "shared-manage-access-secret-value-xx"
	const path = "/api/manage/shareditem/search"
	first := newHTMLManageRouteContext(t, "shop-a", path, secret)
	second := newHTMLManageRouteContext(t, "shop-b", path, secret)

	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(first.Router)
	htmls.AddServiceRouter(second.Router)
	require.NoError(t, htmls.Prepare())
	handler := htmls.Handler()
	require.NotNil(t, handler)

	tok := issueManageToken(t, secret, "admin")
	// 规范路径由 first 持有
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, htmlAuthCount("shop-a:"+path), int32(0))

	// 后到服务仅兼容后缀
	compat := path + "/shop-b"
	req = httptest.NewRequest(http.MethodPost, compat, strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, htmlAuthCount("shop-b:"+path), int32(0))
}

func TestHTMLServerSystemServerGetMenuPublicDoesNotConflictPrepare(t *testing.T) {
	// 复现 04 启动失败：system server 的 release/SystemManage GetMenu 同时出现在
	// ServerManager（canonical getmenu）与 publicAPI；OpenAPI 整服务跳过 server，
	// 同源挂载不得把 system Public/Private 再挂一次导致 path 冲突。
	htmlAuthReset()

	serverSC := newSystemServerLikeServiceContext(t)
	// 业务服务仍须挂载 Public
	const bizPublic = "/api/performanceshop/getproducts"
	biz := newHTMLBusinessServiceContext(t, "performanceshop", bizPublic, "/api/performanceshop/getorder")

	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(serverSC.Router)
	htmls.AddServiceRouter(biz.Router)

	var err error
	require.NotPanics(t, func() {
		err = htmls.Prepare()
	})
	require.NoError(t, err, "canonical getmenu + server public GetMenu 不得冲突")
	handler := htmls.Handler()
	require.NotNil(t, handler)

	// getmenu 规范路径仍可用（ServerManager 挂载，非 public 重挂）
	req := httptest.NewRequest(http.MethodGet, serverManageGetMenuPath, nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, htmlAuthCount("server:"+serverManageGetMenuPath), int32(0))

	// 业务 Public 仍同源可调用
	req = httptest.NewRequest(http.MethodGet, bizPublic, nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, htmlAuthCount("performanceshop:"+bizPublic), int32(0))

	// system server 的 public GetMenu 不得作为 public:server 再暴露；OpenAPI 也不收录 server
	req = httptest.NewRequest(http.MethodGet, "/api/openapi", nil)
	req.Host = "127.0.0.1:48080"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), `"name":"server"`)
	require.Contains(t, rec.Body.String(), bizPublic)
}

// newSystemServerLikeServiceContext 构造与 WebServer 装配等价的 system server 路由集合：
// SystemManage.Routers 含 public.GetMenu（进 publicAPI，path=/api/servermanage/getmenu），
// 再 AddServerRouters(release.Routers()) 把同一路径放进 serverManagerAPI。
// 元数据 key 使用 fixture 前缀，避免污染真实 GetMenu 单例注册表。
func newSystemServerLikeServiceContext(t *testing.T) *router.ServiceContext {
	t.Helper()
	// 与真实 GetMenu 等价：Public PathType + servermanage 路径 + 同 path 的 ServerManager 副本
	publicGetMenu := &markerAuthRouter{}
	publicInfo := &types.RouterInfo{
		Path: serverManageGetMenuPath, Method: http.MethodGet, ServiceName: "server",
		PathType: types.PublicType, InstanceName: "SystemServerPublicGetMenu", StructName: "markerAuthRouter",
		PackPath: "fixture/systemserver/api/public", Auth: false,
	}
	publicGetMenu.info = publicInfo
	publicInfo.SetInstance(publicGetMenu)

	smGetMenu := &markerAuthRouter{}
	smInfo := &types.RouterInfo{
		Path: serverManageGetMenuPath, Method: http.MethodGet, ServiceName: "server",
		PathType: types.ServerManagerType, InstanceName: "SystemServerSMGetMenu", StructName: "markerAuthRouter",
		PackPath: "fixture/systemserver/api/servermanage", Auth: false,
	}
	smGetMenu.info = smInfo
	smInfo.SetInstance(smGetMenu)

	// 额外 release 风格 public（不应被 HTML 同源挂载）
	health := &markerAuthRouter{}
	healthInfo := &types.RouterInfo{
		Path: "/api/servermanage/health", Method: http.MethodGet, ServiceName: "server",
		PathType: types.PublicType, InstanceName: "SystemServerPublicHealth", StructName: "markerAuthRouter",
		PackPath: "fixture/systemserver/api/public", Auth: false,
	}
	health.info = healthInfo
	healthInfo.SetInstance(health)

	service := &htmlAuthService{
		name:   "server",
		routes: []types.IRouter{publicGetMenu, health},
	}
	cfg := config.NewServiceDefaultConfig("server", 18080)
	cfg.Host = "127.0.0.1"
	sc := &router.ServiceContext{
		Config:  cfg,
		Service: &types.Service{Name: "server", Instance: service, Routers: service.routes},
	}
	sc.Router = router.NewServiceRouter(sc, service)
	// 等价 AddServerRouters(release)：GetMenu 进 serverManagerAPI
	sc.Router.AddServerRouters(smGetMenu)
	require.NotNil(t, sc.Router.GetRouter(serverManageGetMenuPath))
	// 确认 publicAPI 与 serverManager 均可见该 path（复现冲突前提）
	var hasPublic, hasSM bool
	for _, info := range sc.Router.GetTypeRouters(types.PublicType) {
		if info != nil && info.GetPath() == serverManageGetMenuPath {
			hasPublic = true
		}
	}
	for _, info := range sc.Router.GetTypeRouters(types.ServerManagerType) {
		if info != nil && info.GetPath() == serverManageGetMenuPath {
			hasSM = true
		}
	}
	require.True(t, hasPublic, "fixture must place GetMenu in publicAPI")
	require.True(t, hasSM, "fixture must place GetMenu in serverManagerAPI")
	return sc
}

func newHTMLManageRouteContext(t *testing.T, name, path, manageSecret string) *router.ServiceContext {
	t.Helper()
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
	require.NotNil(t, sc.Router.GetRouter(path))
	return sc
}
