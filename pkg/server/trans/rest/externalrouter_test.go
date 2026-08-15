package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/digitalwayhk/core/pkg/server/ratelimit"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

type externalRouterService struct {
	name   string
	routes []types.IRouter
}

func (s *externalRouterService) ServiceName() string { return s.name }
func (s *externalRouterService) Routers() []types.IRouter {
	if len(s.routes) == 0 {
		return nil
	}
	return s.routes
}

// localOnlyRouter 模拟 TestToken/ServerArgs：Validation 拒绝非本地访问。
// 注意：Router 对象池会创建新实例，因此不能用实例字段计数；通过响应断言执行链。
type localOnlyRouter struct {
	info *types.RouterInfo
}

func (*localOnlyRouter) Parse(types.IRequest) error { return nil }
func (*localOnlyRouter) Validation(req types.IRequest) error {
	ip := req.GetClientIP()
	if index := strings.Index(ip, ":"); index > 0 {
		ip = ip[:index]
	}
	if !utils.HasLocalIPAddr(ip) {
		return errors.New("服务管理接口只能在本地机访问！")
	}
	return nil
}
func (*localOnlyRouter) Do(types.IRequest) (interface{}, error) {
	return map[string]string{"ok": "local-only"}, nil
}
func (r *localOnlyRouter) RouterInfo() *types.RouterInfo { return r.info }

type recordingDoRouter struct {
	info *types.RouterInfo
}

func (*recordingDoRouter) Parse(types.IRequest) error      { return nil }
func (*recordingDoRouter) Validation(types.IRequest) error { return nil }
func (*recordingDoRouter) Do(types.IRequest) (interface{}, error) {
	return map[string]string{"ok": "rate-limited-route"}, nil
}
func (r *recordingDoRouter) RouterInfo() *types.RouterInfo { return r.info }

func externalRouterTestContext(t *testing.T) (*router.ServiceContext, *types.RouterInfo) {
	t.Helper()
	name := "external-router-" + strings.ReplaceAll(t.Name(), "/", "-")
	path := "/api/servermanage/testtoken"
	api := &localOnlyRouter{}
	info := &types.RouterInfo{
		Path: path, Method: http.MethodGet, ServiceName: name,
		PathType: types.ServerManagerType, InstanceName: "LocalOnly-" + name, StructName: "localOnlyRouter",
		PackPath: "fixture/api/servermanage",
	}
	api.info = info
	info.SetInstance(api)
	service := &externalRouterService{name: name}
	sc := &router.ServiceContext{
		Config:  config.NewServiceDefaultConfig(name, 18080),
		Service: &types.Service{Name: name, Instance: service},
	}
	sc.Config.Host = "127.0.0.1"
	sc.Router = router.NewServiceRouter(sc, service)
	sc.Router.AddServerRouters(api)
	registered := sc.Router.GetRouter(path)
	require.NotNil(t, registered)
	return sc, registered
}

func TestExternalRouterHandlerKeepsRouterValidation(t *testing.T) {
	sc, info := externalRouterTestContext(t)
	req := httptest.NewRequest(http.MethodGet, info.GetPath(), nil)
	req.RemoteAddr = "203.0.113.10:1234"
	rec := httptest.NewRecorder()

	NewExternalRouterHandler(sc, info).ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), `"success":false`)
	require.NotContains(t, rec.Body.String(), "local-only")
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))
	require.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
}

func TestExternalRouterHandlerRejectsWrongMethod(t *testing.T) {
	sc, info := externalRouterTestContext(t)
	req := httptest.NewRequest(http.MethodPost, info.GetPath(), nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()

	NewExternalRouterHandler(sc, info).ServeHTTP(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Equal(t, info.GetMethod(), rec.Header().Get("Allow"))
	require.NotContains(t, rec.Body.String(), "local-only")
}

func TestExternalRouterHandlerKeepsRateLimitAndSecurityHeaders(t *testing.T) {
	name := "external-rate-" + strings.ReplaceAll(t.Name(), "/", "-")
	manager := ratelimit.NewManager(name, time.Minute)
	t.Cleanup(manager.Close)

	api := &recordingDoRouter{}
	info := &types.RouterInfo{
		Path: "/api/callback", Method: http.MethodGet, ServiceName: name,
		PathType: types.PublicType, InstanceName: "RecordingDo-" + name, StructName: "recordingDoRouter",
		PackPath: "fixture/api/public",
	}
	api.info = info
	info.SetInstance(api)
	info.ConfigureExternalRateLimit(types.ExternalRateLimitPolicy{Rate: 1, Burst: 1})

	service := &externalRouterService{name: name, routes: []types.IRouter{api}}
	cfg := config.NewServiceDefaultConfig(name, 18081)
	sc := &router.ServiceContext{
		Config:            cfg,
		Service:           &types.Service{Name: name, Instance: service, Routers: []types.IRouter{api}},
		PublicRateLimiter: manager,
	}
	sc.Router = router.NewServiceRouter(sc, service)
	registered := sc.Router.GetRouter(info.GetPath())
	require.NotNil(t, registered)
	require.NotNil(t, registered.GetExternalRateLimit())

	handler := NewExternalRouterHandler(sc, registered)
	first := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, registered.GetPath(), nil)
	req.RemoteAddr = "198.51.100.10:42000"
	handler.ServeHTTP(first, req)
	require.Equal(t, http.StatusOK, first.Code)
	require.Contains(t, first.Body.String(), "rate-limited-route")

	second := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, registered.GetPath(), nil)
	req.RemoteAddr = "198.51.100.10:42001"
	handler.ServeHTTP(second, req)
	require.Equal(t, http.StatusTooManyRequests, second.Code)
	require.NotContains(t, second.Body.String(), "rate-limited-route")
	require.Equal(t, "nosniff", second.Header().Get("X-Content-Type-Options"))

	var response ErrorResponse
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &response))
	require.Equal(t, types.PublicCodeRateLimited, response.Code)
}

func TestExternalRouterHandlerUsesRouterExecNotDirectDo(t *testing.T) {
	sc, info := externalRouterTestContext(t)

	// 本地访问：经 Parse/Validation/Do（RouterInfo.Exec）返回业务数据。
	req := httptest.NewRequest(http.MethodGet, info.GetPath(), nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	NewExternalRouterHandler(sc, info).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "local-only")
	require.Contains(t, rec.Body.String(), `"success":true`)

	// 非本地：Validation 失败，不得出现业务数据。
	req = httptest.NewRequest(http.MethodGet, info.GetPath(), nil)
	req.RemoteAddr = "203.0.113.10:9"
	rec = httptest.NewRecorder()
	NewExternalRouterHandler(sc, info).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), `"success":false`)
	require.NotContains(t, rec.Body.String(), "local-only")
}

func TestExternalRouterHandlerRecordsLogicalServiceMetrics(t *testing.T) {
	observability.EnableMetrics()
	sc, info := externalRouterTestContext(t)
	labels := map[string]string{
		"service":      observability.NormalizeServiceLabel(sc.Service.Name),
		"route":        info.GetPath(),
		"protocol":     "http",
		"result_class": observability.ResultSuccess,
	}
	before := gatherRESTCounter(t, "core_service_request_requests_total", labels)

	req := httptest.NewRequest(http.MethodGet, info.GetPath(), nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	NewExternalRouterHandler(sc, info).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	after := gatherRESTCounter(t, "core_service_request_requests_total", labels)
	require.Equal(t, before+1, after)
}

func gatherRESTCounter(t *testing.T, name string, want map[string]string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if matchRESTLabels(metric.GetLabel(), want) && metric.GetCounter() != nil {
				return metric.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func matchRESTLabels(got []*dto.LabelPair, want map[string]string) bool {
	values := make(map[string]string, len(got))
	for _, label := range got {
		values[label.GetName()] = label.GetValue()
	}
	for name, value := range want {
		if values[name] != value {
			return false
		}
	}
	return true
}
