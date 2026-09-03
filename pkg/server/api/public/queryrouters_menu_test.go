// 本文件验证 QueryRouters 为跨进程 UpdateMenu 提供完整、稳定的菜单发现快照。
package public

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type queryRoutersParseRequest struct {
	types.IRequest
	body   []byte
	values map[string]string
	caller string
	http   *http.Request
}

func (r *queryRoutersParseRequest) Bind(target interface{}) error {
	if len(r.body) == 0 {
		return nil
	}
	return json.Unmarshal(r.body, target)
}
func (r *queryRoutersParseRequest) GetValue(key string) string { return r.values[key] }
func (r *queryRoutersParseRequest) TrustedInternalCaller() (string, bool) {
	return r.caller, r.caller != ""
}
func (r *queryRoutersParseRequest) Authorized() bool              { return false }
func (r *queryRoutersParseRequest) GetClientIP() string           { return "203.0.113.8" }
func (r *queryRoutersParseRequest) ServiceName() string           { return "missing-service" }
func (r *queryRoutersParseRequest) GetHttpRequest() *http.Request { return r.http }

type menuSnapshotManage struct {
	info *types.RouterInfo
}

func (*menuSnapshotManage) Parse(types.IRequest) error      { return nil }
func (*menuSnapshotManage) Validation(types.IRequest) error { return nil }
func (*menuSnapshotManage) Do(types.IRequest) (interface{}, error) {
	return nil, nil
}
func (r *menuSnapshotManage) RouterInfo() *types.RouterInfo { return r.info }
func (*menuSnapshotManage) GetTitle() string                { return "用户管理" }
func (*menuSnapshotManage) GetLocaleTitle(current string) string {
	if current == "en-US" {
		return "Users"
	}
	return "用户管理"
}

type menuSnapshotService struct {
	name  string
	route types.IRouter
}

type menuSnapshotRequest struct {
	types.IRequest
	serviceName string
}

func (r *menuSnapshotRequest) ServiceName() string { return r.serviceName }

func (s *menuSnapshotService) ServiceName() string      { return s.name }
func (s *menuSnapshotService) Routers() []types.IRouter { return []types.IRouter{s.route} }
func (*menuSnapshotService) GetTitle() string           { return "用户服务" }
func (*menuSnapshotService) GetLocaleTitle(current string) string {
	if current == "en-US" {
		return "User Service"
	}
	return "用户服务"
}

func TestNewMenuServiceSnapshotIncludesLocalizedRoutesAndReports(t *testing.T) {
	stats.ResetReportsForTest()
	t.Cleanup(stats.ResetReportsForTest)

	const serviceName = "menu-snapshot-users"
	route := &menuSnapshotManage{}
	route.info = &types.RouterInfo{
		Path:         "/api/manage/menu-snapshot-users/usermanage/search",
		ServiceName:  serviceName,
		PathType:     types.ManageType,
		InstanceName: "UserManage",
		Method:       "POST",
	}
	route.info.SetInstance(route)

	cfg := config.NewServiceDefaultConfig(serviceName, 0)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	cfg.Transport.Internal = ""
	cfg.Transport.Fallback = nil
	sc := router.NewServiceContextWithConfig(&menuSnapshotService{name: serviceName, route: route}, cfg)
	t.Cleanup(func() { sc.SetRunState(false) })

	stats.RegisterReports(stats.ReportDef{
		Code: "login-daily", Service: serviceName, Title: "每日登录",
		SpecCode: "users.login_daily", Kind: stats.ReportKindLine,
	})

	snapshot := NewMenuServiceSnapshot(sc)
	require.Equal(t, serviceName, snapshot.Name)
	require.Equal(t, "用户服务", snapshot.Title)
	require.Equal(t, "User Service", snapshot.TitleEN)
	require.Len(t, snapshot.Routers, 1)
	require.Equal(t, "/api/manage/menu-snapshot-users/usermanage/search", snapshot.Routers[0].Path)
	require.Equal(t, "UserManage", snapshot.Routers[0].InstanceName)
	require.Equal(t, "用户管理", snapshot.Routers[0].Title)
	require.Equal(t, "Users", snapshot.Routers[0].TitleEN)
	require.Len(t, snapshot.Reports, 1)
	require.Equal(t, "login-daily", snapshot.Reports[0].Code)

	req := &menuSnapshotRequest{serviceName: serviceName}
	legacy, err := (&QueryRouters{ApiType: 3}).Do(req)
	require.NoError(t, err)
	require.IsType(t, []*types.RouterInfo{}, legacy)
	menuResult, err := (&QueryRouters{ApiType: 3, ForMenu: true}).Do(req)
	require.NoError(t, err)
	require.IsType(t, &smodels.MenuServiceSnapshot{}, menuResult)
}

func TestQueryRoutersParseAcceptsMenuSnapshotJSONAndQueryOverrides(t *testing.T) {
	query := &QueryRouters{}
	sc := &router.ServiceContext{
		Service: &types.Service{Name: "users"},
		Config:  config.NewServiceDefaultConfig("users", 0),
	}
	serviceRouter := router.NewServiceRouter(sc, nil)
	httpRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/servermanage/queryrouters?apitype=3",
		strings.NewReader(`{"apiType":2,"forMenu":true}`),
	)
	req := router.NewRequest(serviceRouter, httpRequest)
	require.NoError(t, query.Parse(req))
	require.Equal(t, 3, query.ApiType)
	require.True(t, query.ForMenu)
}

func TestQueryRoutersMenuSnapshotAllowsTrustedInternalCaller(t *testing.T) {
	query := &QueryRouters{ForMenu: true}
	require.NoError(t, query.Validation(&queryRoutersParseRequest{caller: "exchange"}))

	err := query.Validation(&queryRoutersParseRequest{})
	require.Error(t, err)
	require.False(t, errors.Is(err, types.ErrInternalCallerForbidden))
}
