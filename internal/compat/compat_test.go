package compat

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type fixtureRouter struct {
	Name string `json:"name" desc:"名称"`
	info *types.RouterInfo
}

func (r *fixtureRouter) Parse(types.IRequest) error      { return nil }
func (r *fixtureRouter) Validation(types.IRequest) error { return nil }
func (r *fixtureRouter) Do(types.IRequest) (interface{}, error) {
	return map[string]string{"status": "ok"}, nil
}
func (r *fixtureRouter) RouterInfo() *types.RouterInfo { return r.info }
func (r *fixtureRouter) GetResponse() interface{}      { return map[string]string{"status": "ok"} }

type fixtureService struct {
	name    string
	routers []types.IRouter
}

func (s *fixtureService) ServiceName() string                    { return s.name }
func (s *fixtureService) Routers() []types.IRouter               { return s.routers }
func (s *fixtureService) SubscribeRouters() []*types.ObserveArgs { return nil }

func newFixtureServiceRouter(name string, port int, specs ...RouteEntry) *router.ServiceRouter {
	routers := make([]types.IRouter, 0, len(specs))
	for _, spec := range specs {
		api := &fixtureRouter{}
		api.info = &types.RouterInfo{
			Path:         spec.Path,
			Method:       spec.Method,
			Auth:         spec.Auth,
			ServiceName:  spec.Service,
			PathType:     types.ApiType(spec.PathType),
			StructName:   "fixtureRouter",
			InstanceName: "fixtureRouter",
			Subscriber:   make(map[types.ObserveState]map[string]*types.ObserveArgs),
		}
		api.info.SetInstance(api)
		routers = append(routers, api)
	}
	service := &fixtureService{name: name, routers: routers}
	ctx := &router.ServiceContext{
		Config:  &config.ServerConfig{RestConf: config.NewServiceDefaultConfig(name, port).RestConf},
		Service: &types.Service{Name: name, Routers: routers, Instance: service},
	}
	ctx.Config.Name = name
	ctx.Config.Port = port
	ctx.Router = router.NewServiceRouter(ctx, service)
	return ctx.Router
}

func TestRouteSnapshotIsSortedAndMatchesGolden(t *testing.T) {
	sr := newFixtureServiceRouter("compat", 18080,
		RouteEntry{Service: "compat", Method: http.MethodPost, Path: "/api/compat/private", PathType: string(types.PrivateType), Auth: true},
		RouteEntry{Service: "compat", Method: http.MethodGet, Path: "/api/compat/public", PathType: string(types.PublicType)},
	)

	got, err := SnapshotRoutes(sr)
	require.NoError(t, err)
	requireGolden(t, "routes.golden.json", got)
}

func TestRouteSnapshotRejectsMethodPathConflictAcrossServices(t *testing.T) {
	first := newFixtureServiceRouter("first", 18081,
		RouteEntry{Service: "first", Method: http.MethodGet, Path: "/api/shared/path", PathType: string(types.PublicType)},
	)
	second := newFixtureServiceRouter("second", 18082,
		RouteEntry{Service: "second", Method: http.MethodGet, Path: "/api/shared/path", PathType: string(types.PublicType)},
	)

	_, err := SnapshotRoutes(first, second)
	require.ErrorContains(t, err, "duplicate route GET /api/shared/path")
}

func TestOpenAPISnapshotIgnoresRuntimeHostAndPort(t *testing.T) {
	first := newFixtureServiceRouter("compat", 18080,
		RouteEntry{Service: "compat", Method: http.MethodGet, Path: "/api/compat/public", PathType: string(types.PublicType)},
	)
	second := newFixtureServiceRouter("compat", 28080,
		RouteEntry{Service: "compat", Method: http.MethodGet, Path: "/api/compat/public", PathType: string(types.PublicType)},
	)

	one, err := SnapshotOpenAPI(requestWithHost("first.example:18080"), first)
	require.NoError(t, err)
	two, err := SnapshotOpenAPI(requestWithHost("second.example:28080"), second)
	require.NoError(t, err)
	require.Equal(t, one, two)
	requireGolden(t, "openapi.golden.json", one)
}

func TestOpenAPISnapshotAllowsEmptyServices(t *testing.T) {
	require.NotPanics(t, func() {
		got, err := SnapshotOpenAPI(requestWithHost("compat.example"))
		require.NoError(t, err)
		require.Contains(t, string(got), `"paths": {}`)
	})
}

func TestProductionOpenAPIDoesNotPanicWithoutServices(t *testing.T) {
	require.NotPanics(t, func() {
		doc := run.GetOpenApi(requestWithHost("compat.example"))
		require.NotNil(t, doc)
	})
}

func requestWithHost(host string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "http://"+host+"/api/openapi", nil)
	return req
}

func requireGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, got, 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "golden 缺失时必须显式创建，测试不得自动覆盖")
	require.Equal(t, string(want), string(got))
}
