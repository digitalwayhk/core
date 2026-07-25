package run

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

var htmlAuthProxyPaths = []struct {
	path   string
	method string
}{
	{"/api/servermanage/testtoken", http.MethodGet},
	{"/api/casdoor", http.MethodGet},
	{"/api/casdoor/callback", http.MethodGet},
	{"/api/refresh", http.MethodPost},
}

type htmlAuthService struct {
	name   string
	routes []types.IRouter
}

func (s *htmlAuthService) ServiceName() string      { return s.name }
func (s *htmlAuthService) Routers() []types.IRouter { return s.routes }

// 对象池会复制 Router 实例，用服务名+路径的全局计数器证明调用归属。
var htmlAuthCallMu sync.Mutex
var htmlAuthCalls = map[string]*atomic.Int32{}

func htmlAuthCount(key string) int32 {
	htmlAuthCallMu.Lock()
	defer htmlAuthCallMu.Unlock()
	if htmlAuthCalls[key] == nil {
		return 0
	}
	return htmlAuthCalls[key].Load()
}

func htmlAuthInc(key string) {
	htmlAuthCallMu.Lock()
	defer htmlAuthCallMu.Unlock()
	if htmlAuthCalls[key] == nil {
		htmlAuthCalls[key] = &atomic.Int32{}
	}
	htmlAuthCalls[key].Add(1)
}

func htmlAuthReset() {
	htmlAuthCallMu.Lock()
	defer htmlAuthCallMu.Unlock()
	htmlAuthCalls = map[string]*atomic.Int32{}
}

type markerAuthRouter struct {
	info *types.RouterInfo
}

func (*markerAuthRouter) Parse(types.IRequest) error      { return nil }
func (*markerAuthRouter) Validation(types.IRequest) error { return nil }
func (*markerAuthRouter) Do(req types.IRequest) (interface{}, error) {
	service := req.ServiceName()
	path := req.GetPath()
	htmlAuthInc(service + ":" + path)
	return map[string]string{"service": service, "path": path}, nil
}
func (r *markerAuthRouter) RouterInfo() *types.RouterInfo { return r.info }

func newHTMLAuthServiceContext(t *testing.T, name string, manage bool) *router.ServiceContext {
	t.Helper()
	var manageRoutes []types.IRouter
	if manage {
		path := "/api/manage/" + strings.ToLower(name) + "/item"
		api := &markerAuthRouter{}
		info := &types.RouterInfo{
			Path: path, Method: http.MethodPost, ServiceName: name,
			PathType: types.ManageType, InstanceName: "Manage-" + name, StructName: "markerAuthRouter",
			PackPath: "fixture/api/manage",
		}
		api.info = info
		info.SetInstance(api)
		manageRoutes = []types.IRouter{api}
	}
	service := &htmlAuthService{name: name, routes: manageRoutes}
	cfg := config.NewServiceDefaultConfig(name, 18080)
	cfg.Host = "127.0.0.1"
	cfg.ManageAuth.AccessSecret = "shared-manage-access-secret"
	cfg.ManageAuth.RefreshSecret = "shared-manage-refresh-secret"
	cfg.ManageAuth.AccessExpire = 7200
	cfg.ManageAuth.RefreshExpire = 2592000
	sc := &router.ServiceContext{
		Config:  cfg,
		Service: &types.Service{Name: name, Instance: service, Routers: manageRoutes},
	}
	sc.Router = router.NewServiceRouter(sc, service)

	for _, p := range htmlAuthProxyPaths {
		api := &markerAuthRouter{}
		info := &types.RouterInfo{
			Path: p.path, Method: p.method, ServiceName: name,
			PathType:     types.ServerManagerType,
			InstanceName: "Auth-" + name + "-" + strings.ReplaceAll(p.path, "/", "_"),
			StructName:   "markerAuthRouter",
			PackPath:     "fixture/api/servermanage",
		}
		api.info = info
		info.SetInstance(api)
		sc.Router.AddServerRouters(api)
		require.NotNil(t, sc.Router.GetRouter(p.path), "path %s", p.path)
	}
	return sc
}

func TestHTMLServerAuthRoutesBindAuthorityWithoutServiceSuffix(t *testing.T) {
	htmlAuthReset()
	authority := newHTMLAuthServiceContext(t, "orders", true)
	peer := newHTMLAuthServiceContext(t, "users", true)

	htmls := NewHTMLServer(0)
	htmls.SetManageAuthAuthority(&manageAuthAuthority{
		name:    "orders",
		context: authority,
		router:  authority.Router,
	})
	htmls.AddServiceRouter(authority.Router)
	htmls.AddServiceRouter(peer.Router)
	require.NoError(t, htmls.Prepare())

	handler := htmls.Handler()
	require.NotNil(t, handler)

	for _, p := range htmlAuthProxyPaths {
		t.Run(p.path, func(t *testing.T) {
			// 固定路径、无服务名后缀
			require.False(t, strings.HasSuffix(p.path, "/orders"))
			require.False(t, strings.Contains(p.path, "/orders/"))

			req := httptest.NewRequest(p.method, p.path, nil)
			req.RemoteAddr = "127.0.0.1:9"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), `"service":"orders"`)
			require.NotContains(t, rec.Body.String(), `"service":"users"`)
			require.Greater(t, htmlAuthCount("orders:"+p.path), int32(0))
			require.Equal(t, int32(0), htmlAuthCount("users:"+p.path), "不得调用非权威服务同名 Router")
		})
	}
}

func TestHTMLServerAuthPrepareFailsClosedWhenRouterMissing(t *testing.T) {
	name := "orders-missing-refresh"
	service := &htmlAuthService{name: name}
	cfg := config.NewServiceDefaultConfig(name, 18080)
	sc := &router.ServiceContext{
		Config:  cfg,
		Service: &types.Service{Name: name, Instance: service},
	}
	sc.Router = router.NewServiceRouter(sc, service)
	for _, p := range htmlAuthProxyPaths[:3] {
		api := &markerAuthRouter{}
		info := &types.RouterInfo{
			Path: p.path, Method: p.method, ServiceName: name,
			PathType:     types.ServerManagerType,
			InstanceName: "Partial-" + name + "-" + strings.ReplaceAll(p.path, "/", "_"),
			StructName:   "markerAuthRouter",
			PackPath:     "fixture/api/servermanage",
		}
		api.info = info
		info.SetInstance(api)
		sc.Router.AddServerRouters(api)
	}

	htmls := NewHTMLServer(0)
	htmls.SetManageAuthAuthority(&manageAuthAuthority{
		name: name, context: sc, router: sc.Router,
	})
	err := htmls.Prepare()
	require.Error(t, err)
	require.ErrorContains(t, err, name)
	require.ErrorContains(t, err, "/api/refresh")
	require.NotContains(t, err.Error(), "shared-manage-access-secret")
	require.NotContains(t, err.Error(), "Bearer")
	require.Nil(t, htmls.Handler())
}

func TestHTMLServerHandlerNilBeforePrepare(t *testing.T) {
	htmls := NewHTMLServer(0)
	require.Nil(t, htmls.Handler())
}

// TestHTMLServerStartDoesNotImplicitlyPrepare 证明 Start 未 Prepare 时 fail closed，
// 不会惰性调用 Prepare；不依赖真实监听端口。
func TestHTMLServerStartDoesNotImplicitlyPrepare(t *testing.T) {
	server := NewHTMLServer(1)
	require.Nil(t, server.Handler())
	require.False(t, server.prepared)

	// 与 Start 相同的决策入口：只读缓存，不 Prepare。
	require.Nil(t, server.startHTTPHandler())
	require.False(t, server.prepared)
	require.Nil(t, server.Handler())

	// 走完整 Start 路径：Isstart 放行后因未 Prepare 立即返回，不 Listen。
	server.Isstart <- true
	done := make(chan struct{})
	go func() {
		server.Start()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("未 Prepare 的 Start 应立即返回，不得隐式 Prepare 后监听")
	}
	require.False(t, server.prepared)
	require.Nil(t, server.Handler())
	require.Nil(t, server.startHTTPHandler())
}
