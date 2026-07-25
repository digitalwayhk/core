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

func TestWebBootstrapModeMatrix(t *testing.T) {
	tests := []struct {
		name        string
		casdoor     bool
		remoteAddr  string
		wantMode    string
		wantAcquire bool
		wantCasdoor bool
		wantLogin   bool
		wantLogout  bool
		wantTestID  bool
	}{
		{
			name: "casdoor on local allow", casdoor: true, remoteAddr: "127.0.0.1:9",
			wantMode: webAuthModeCasdoor, wantAcquire: false, wantCasdoor: true,
			wantLogin: true, wantLogout: true, wantTestID: false,
		},
		{
			name: "casdoor on local deny", casdoor: true, remoteAddr: "203.0.113.10:9",
			wantMode: webAuthModeCasdoor, wantAcquire: false, wantCasdoor: true,
			wantLogin: true, wantLogout: true, wantTestID: false,
		},
		{
			name: "casdoor off local allow", casdoor: false, remoteAddr: "127.0.0.1:9",
			wantMode: webAuthModeTestToken, wantAcquire: true, wantCasdoor: false,
			wantLogin: false, wantLogout: false, wantTestID: true,
		},
		{
			name: "casdoor off local deny", casdoor: false, remoteAddr: "203.0.113.10:9",
			wantMode: webAuthModeUnavailable, wantAcquire: false, wantCasdoor: false,
			wantLogin: false, wantLogout: false, wantTestID: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			authorityCtx := newBootstrapAuthorityContext(t, "shop", tc.casdoor)
			htmls := NewHTMLServer(0)
			htmls.SetManageAuthAuthority(&manageAuthAuthority{
				name: "shop", context: authorityCtx, router: authorityCtx.Router,
			})
			htmls.AddServiceRouter(authorityCtx.Router)
			require.NoError(t, htmls.Prepare())

			req := httptest.NewRequest(http.MethodGet, webBootstrapPath, nil)
			req.RemoteAddr = tc.remoteAddr
			rec := httptest.NewRecorder()
			htmls.Handler().ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

			var response WebBootstrap
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			require.Equal(t, 1, response.SchemaVersion)
			require.Equal(t, tc.wantMode, response.Auth.Mode)
			require.Equal(t, webAuthTypeManage, response.Auth.Type)
			require.Equal(t, "shop", response.Auth.AuthorityService)
			require.Equal(t, webBootstrapCallback, response.Endpoints.Callback)
			require.Equal(t, webBootstrapRefresh, response.Endpoints.Refresh)
			require.Equal(t, webBootstrapOpenAPI, response.Endpoints.OpenAPI)
			require.Equal(t, tc.wantLogin, response.UI.ShowLogin)
			require.Equal(t, tc.wantLogout, response.UI.ShowLogout)
			require.Equal(t, tc.wantTestID, response.UI.ShowTestIdentity)

			if tc.wantAcquire {
				require.NotNil(t, response.Endpoints.AcquireToken)
				require.Equal(t, webBootstrapAcquireToken, *response.Endpoints.AcquireToken)
			} else {
				require.Nil(t, response.Endpoints.AcquireToken)
			}
			if tc.wantCasdoor {
				require.NotNil(t, response.Endpoints.CasdoorConfig)
				require.Equal(t, webBootstrapCasdoorConfig, *response.Endpoints.CasdoorConfig)
			} else {
				require.Nil(t, response.Endpoints.CasdoorConfig)
			}

			body := rec.Body.String()
			require.NotContains(t, body, authorityCtx.Config.ManageAuth.AccessSecret)
			require.NotContains(t, body, authorityCtx.Config.ManageAuth.RefreshSecret)
			require.NotContains(t, body, "client_secret")
			if secret := authorityCtx.Config.ManageAuth.CasDoor.WebhookSecret; secret != "" {
				require.NotContains(t, body, secret)
			}
		})
	}
}

func TestWebBootstrapNormalizesAuthorityServiceLikeTokenClaim(t *testing.T) {
	// HTTP：authority.name 带空格/大小写时，响应为 ToLower(TrimSpace(...))。
	// 注册名保持稳定，不改 Service.Name，避免 Prepare 拼接非法 mux 路径。
	ctx := newBootstrapAuthorityContext(t, "orders-norm", false)
	htmls := NewHTMLServer(0)
	htmls.SetManageAuthAuthority(&manageAuthAuthority{
		name: "  Orders  ", context: ctx, router: ctx.Router,
	})
	htmls.AddServiceRouter(ctx.Router)
	require.NoError(t, htmls.Prepare())

	req := httptest.NewRequest(http.MethodGet, webBootstrapPath, nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	htmls.Handler().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var response WebBootstrap
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, "orders", response.Auth.AuthorityService)
	require.Equal(t, strings.ToLower(strings.TrimSpace("  Orders  ")), response.Auth.AuthorityService)

	// 与 Callback / Token Claim 相同公式；name 为空时回退 context.Service.Name。
	require.Equal(t, "shopservice", normalizeBootstrapAuthorityService(&manageAuthAuthority{
		name: "ShopService",
	}))
	require.Equal(t, "users", normalizeBootstrapAuthorityService(&manageAuthAuthority{
		name: "  ",
		context: &router.ServiceContext{
			Service: &types.Service{Name: "  Users  "},
		},
	}))
	require.Equal(t, "", normalizeBootstrapAuthorityService(nil))
}

func TestWebBootstrapSetsNoStoreBeforeMethodCheck(t *testing.T) {
	htmls := NewHTMLServer(0)
	require.NoError(t, htmls.Prepare())
	req := httptest.NewRequest(http.MethodPost, webBootstrapPath, nil)
	rec := httptest.NewRecorder()
	htmls.Handler().ServeHTTP(rec, req)
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestWebBootstrapUnavailableWhenAuthorityNil(t *testing.T) {
	htmls := NewHTMLServer(0)
	require.NoError(t, htmls.Prepare())

	req := httptest.NewRequest(http.MethodGet, webBootstrapPath, nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	htmls.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	var response WebBootstrap
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, webAuthModeUnavailable, response.Auth.Mode)
	require.Equal(t, webAuthTypeManage, response.Auth.Type)
	require.Empty(t, response.Auth.AuthorityService)
	require.Nil(t, response.Endpoints.AcquireToken)
	require.Nil(t, response.Endpoints.CasdoorConfig)
	require.False(t, response.UI.ShowLogin)
	require.False(t, response.UI.ShowLogout)
	require.False(t, response.UI.ShowTestIdentity)
}

func TestHTMLServerOpenAPIAggregatesPublicAndPrivateOnly(t *testing.T) {
	name := "openapi-agg"
	service := &htmlAuthService{name: name}
	cfg := config.NewServiceDefaultConfig(name, 19090)
	cfg.Host = "127.0.0.1"
	sc := &router.ServiceContext{
		Config:  cfg,
		Service: &types.Service{Name: name, Instance: service},
	}

	publicAPI := &markerAuthRouter{}
	publicInfo := &types.RouterInfo{
		Path: "/api/" + name + "/publicitem", Method: http.MethodPost, ServiceName: name,
		PathType: types.PublicType, InstanceName: "PublicItem-" + name, StructName: "markerAuthRouter",
		PackPath: "fixture/api/public",
	}
	publicAPI.info = publicInfo
	publicInfo.SetInstance(publicAPI)

	privateAPI := &markerAuthRouter{}
	privateInfo := &types.RouterInfo{
		Path: "/api/" + name + "/privateitem", Method: http.MethodPost, ServiceName: name,
		PathType: types.PrivateType, InstanceName: "PrivateItem-" + name, StructName: "markerAuthRouter",
		PackPath: "fixture/api/private", Auth: true,
	}
	privateAPI.info = privateInfo
	privateInfo.SetInstance(privateAPI)

	manageAPI := &markerAuthRouter{}
	manageInfo := &types.RouterInfo{
		Path: "/api/manage/" + name + "/manageitem", Method: http.MethodPost, ServiceName: name,
		PathType: types.ManageType, InstanceName: "ManageItem-" + name, StructName: "markerAuthRouter",
		PackPath: "fixture/api/manage",
	}
	manageAPI.info = manageInfo
	manageInfo.SetInstance(manageAPI)

	service.routes = []types.IRouter{publicAPI, privateAPI, manageAPI}
	sc.Service.Routers = service.routes
	sc.Router = router.NewServiceRouter(sc, service)

	htmls := NewHTMLServer(0)
	htmls.AddServiceRouter(sc.Router)
	require.NoError(t, htmls.Prepare())
	handler := htmls.Handler()
	require.NotNil(t, handler)

	openapiReq := httptest.NewRequest(http.MethodGet, "/api/openapi", nil)
	openapiReq.Host = "127.0.0.1:19090"
	openapiRec := httptest.NewRecorder()
	handler.ServeHTTP(openapiRec, openapiReq)
	require.Equal(t, http.StatusOK, openapiRec.Code)

	var doc openapi3.T
	require.NoError(t, json.Unmarshal(openapiRec.Body.Bytes(), &doc))
	require.NotNil(t, doc.Paths)
	require.NotNil(t, doc.Paths.Find("/api/"+name+"/publicitem"))
	require.NotNil(t, doc.Paths.Find("/api/"+name+"/privateitem"))
	require.Nil(t, doc.Paths.Find("/api/manage/"+name+"/manageitem"), "OpenAPI 不得包含 Manage 路径")
	body := openapiRec.Body.String()
	require.NotContains(t, body, "/api/manage/"+name+"/manageitem")
	require.Contains(t, body, "/api/"+name+"/publicitem")
	require.Contains(t, body, "/api/"+name+"/privateitem")

	swaggerReq := httptest.NewRequest(http.MethodGet, "/swagger/", nil)
	swaggerRec := httptest.NewRecorder()
	handler.ServeHTTP(swaggerRec, swaggerReq)
	require.Equal(t, http.StatusOK, swaggerRec.Code)
}

func newBootstrapAuthorityContext(t *testing.T, name string, casdoorEnable bool) *router.ServiceContext {
	t.Helper()
	// 复用 htmlserver_auth 的服务上下文，并补齐认证代理路径与 Casdoor 开关。
	sc := newHTMLAuthServiceContext(t, name, true)
	sc.Config.ManageAuth.AccessSecret = "bootstrap-access-secret-value"
	sc.Config.ManageAuth.RefreshSecret = "bootstrap-refresh-secret-value"
	sc.Config.ManageAuth.CasDoor.Enable = casdoorEnable
	if casdoorEnable {
		sc.Config.ManageAuth.CasDoor.WebhookSecret = "bootstrap-webhook-secret"
		// Yaml 不需要在 bootstrap 模式选择中加载；Enable 即可。
	}
	return sc
}
