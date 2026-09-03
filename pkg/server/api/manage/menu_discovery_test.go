// 本文件验证 UpdateMenu 通过集群发现聚合独立进程的 Manage 菜单，并在缺失服务时失败闭合。
package manage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type menuDiscoveryResponse struct {
	data interface{}
	err  error
}

func (r *menuDiscoveryResponse) GetSuccess() bool { return r.err == nil }
func (r *menuDiscoveryResponse) GetMessage() string {
	if r.err == nil {
		return ""
	}
	return r.err.Error()
}
func (r *menuDiscoveryResponse) GetData(target ...interface{}) interface{} {
	if len(target) == 0 {
		return r.data
	}
	raw, _ := json.Marshal(r.data)
	_ = json.Unmarshal(raw, target[0])
	return target[0]
}
func (r *menuDiscoveryResponse) GetError() error { return r.err }

type menuDiscoveryRequest struct {
	types.IRequest
	source    *router.ServiceContext
	remote    map[string]*smodels.MenuServiceSnapshot
	remoteErr map[string]error
	calls     []string
}

func (r *menuDiscoveryRequest) GetService() *router.ServiceContext { return r.source }
func (r *menuDiscoveryRequest) ServiceName() string                { return r.source.Service.Name }
func (r *menuDiscoveryRequest) GetTargetServerInfo(service string) *types.TargetInfo {
	return &types.TargetInfo{TargetService: service, TargetAddress: service, TargetPort: 8080}
}
func (r *menuDiscoveryRequest) CallTargetService(api types.IRouter, target *types.TargetInfo, _ ...func(types.IResponse)) (types.IResponse, error) {
	query, ok := api.(*public.QueryRouters)
	if !ok || query.ApiType != 3 || !query.ForMenu {
		return nil, errors.New("unexpected menu discovery request")
	}
	r.calls = append(r.calls, target.TargetService)
	if err := r.remoteErr[target.TargetService]; err != nil {
		return nil, err
	}
	return &menuDiscoveryResponse{data: r.remote[target.TargetService]}, nil
}

func (r *menuDiscoveryRequest) querySnapshot(_ context.Context, _ types.IRequest, _ *router.ServiceContext, service string, _ *types.TargetInfo) (*smodels.MenuServiceSnapshot, error) {
	r.calls = append(r.calls, service)
	if err := r.remoteErr[service]; err != nil {
		return nil, err
	}
	return r.remote[service], nil
}

func newMenuDiscoveryProvider(t *testing.T, services ...string) cluster.DiscoveryProvider {
	t.Helper()
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	t.Cleanup(func() { require.NoError(t, provider.Close()) })
	for i, service := range services {
		require.NoError(t, provider.Register(context.Background(), &cluster.NodeInfo{
			ID: fmt.Sprintf("%s-%d", service, i+1), ServiceName: service,
			ServiceInstanceID: fmt.Sprintf("%s-%d", service, i+1), Status: cluster.NodeStatusRunning,
			Address: service, Port: 8080, DataCenterID: 1, MachineID: int64(i + 1),
		}))
	}
	return provider
}

func TestDiscoverMenuServiceSnapshotsAggregatesRemoteServices(t *testing.T) {
	provider := newMenuDiscoveryProvider(t, "exchange", "users")
	source := &router.ServiceContext{
		Service: &types.Service{Name: "exchange"}, ServiceInstanceID: "exchange-1",
		ClusterProvider: provider,
	}
	req := &menuDiscoveryRequest{
		source: source,
		remote: map[string]*smodels.MenuServiceSnapshot{
			"users": {Name: "users", Routers: []smodels.MenuRouterSnapshot{{
				Path: "/api/manage/users/usermanage/search", InstanceName: "UserManage",
			}}},
		},
		remoteErr: map[string]error{},
	}

	snapshots, err := discoverMenuServiceSnapshotsFrom(req, map[string]*router.ServiceContext{"exchange": source}, req.querySnapshot)
	require.NoError(t, err)
	require.Len(t, snapshots, 2)
	require.Equal(t, "exchange", snapshots[0].Name)
	require.Equal(t, "users", snapshots[1].Name)
	require.Equal(t, []string{"users"}, req.calls)
}

func TestDiscoverMenuServiceSnapshotsUsesBusinessContextDiscovery(t *testing.T) {
	serverProvider := newMenuDiscoveryProvider(t, "server")
	businessProvider := newMenuDiscoveryProvider(t, "exchange", "users")
	serverContext := &router.ServiceContext{
		Service: &types.Service{Name: "server"}, ClusterProvider: serverProvider,
	}
	businessContext := &router.ServiceContext{
		Service: &types.Service{Name: "exchange"}, ServiceInstanceID: "exchange-1",
		ClusterProvider: businessProvider,
	}
	req := &menuDiscoveryRequest{source: serverContext}
	local := map[string]*router.ServiceContext{
		"server": serverContext, "exchange": businessContext,
	}
	var authority string
	snapshots, err := discoverMenuServiceSnapshotsFrom(req, local, func(
		_ context.Context, _ types.IRequest, sc *router.ServiceContext, serviceName string, _ *types.TargetInfo,
	) (*smodels.MenuServiceSnapshot, error) {
		authority = sc.Service.Name
		return &smodels.MenuServiceSnapshot{Name: serviceName, Routers: []smodels.MenuRouterSnapshot{{
			Path: "/api/manage/users/usermanage/search", InstanceName: "UserManage",
		}}}, nil
	})

	require.NoError(t, err)
	require.Len(t, snapshots, 2)
	require.Equal(t, "exchange", authority)
}

func TestDiscoverMenuServiceSnapshotsSupportsLocalOnlyBusinessContext(t *testing.T) {
	source := &router.ServiceContext{Service: &types.Service{Name: "exchange"}}
	called := false
	snapshots, err := discoverMenuServiceSnapshotsFrom(
		&menuDiscoveryRequest{source: source},
		map[string]*router.ServiceContext{"exchange": source},
		func(context.Context, types.IRequest, *router.ServiceContext, string, *types.TargetInfo) (*smodels.MenuServiceSnapshot, error) {
			called = true
			return nil, errors.New("unexpected remote query")
		},
	)
	require.NoError(t, err)
	require.Len(t, snapshots, 1)
	require.Equal(t, "exchange", snapshots[0].Name)
	require.False(t, called)
}

func TestValidateMenuServiceSnapshotRejectsCrossServiceData(t *testing.T) {
	tests := []struct {
		name     string
		snapshot *smodels.MenuServiceSnapshot
	}{
		{name: "route", snapshot: &smodels.MenuServiceSnapshot{Name: "users", Routers: []smodels.MenuRouterSnapshot{{
			Path: "/api/manage/funds/fundmanage/search", InstanceName: "FundManage",
		}}}},
		{name: "report", snapshot: &smodels.MenuServiceSnapshot{Name: "users", Reports: []stats.ReportMenuItem{{
			Service: "funds", Code: "daily", Path: stats.ReportPath("funds", "daily"),
		}}}},
		{name: "dot-segment", snapshot: &smodels.MenuServiceSnapshot{Name: "users", Routers: []smodels.MenuRouterSnapshot{{
			Path: "/api/manage/users/../funds/search", InstanceName: "UserManage",
		}}}},
		{name: "encoded-path", snapshot: &smodels.MenuServiceSnapshot{Name: "users", Routers: []smodels.MenuRouterSnapshot{{
			Path: "/api/manage/users/usermanage/%2e%2e", InstanceName: "UserManage",
		}}}},
		{name: "report-code", snapshot: &smodels.MenuServiceSnapshot{Name: "users", Reports: []stats.ReportMenuItem{{
			Service: "users", Code: "../daily", Path: stats.ReportPath("users", "../daily"),
		}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, validateMenuServiceSnapshot(tt.snapshot, "users"))
		})
	}
}

func TestValidateMenuServiceSnapshotRejectsOversizedSnapshot(t *testing.T) {
	snapshot := &smodels.MenuServiceSnapshot{
		Name:    "users",
		Routers: make([]smodels.MenuRouterSnapshot, maxMenuSnapshotRouters+1),
	}
	require.ErrorContains(t, validateMenuServiceSnapshot(snapshot, "users"), "size limits")
}

func TestMenuQueryRoutersUsesFixedTargetMetadata(t *testing.T) {
	query := newMenuQueryRouters("users")
	info := query.RouterInfo()
	require.Equal(t, "/api/servermanage/queryrouters", info.GetPath())
	require.Equal(t, "users", info.GetServiceName())
	require.Equal(t, types.ServerManagerType, info.GetPathType())
	require.Equal(t, 3, query.ApiType)
	require.True(t, query.ForMenu)
}

func TestDiscoverMenuServiceSnapshotsRejectsDifferentRunningNodeSnapshots(t *testing.T) {
	provider := newMenuDiscoveryProvider(t, "exchange", "users", "users")
	source := &router.ServiceContext{
		Service: &types.Service{Name: "exchange"}, ServiceInstanceID: "exchange-1",
		ClusterProvider: provider,
	}
	call := 0
	_, err := discoverMenuServiceSnapshotsFrom(
		&menuDiscoveryRequest{source: source},
		map[string]*router.ServiceContext{"exchange": source},
		func(_ context.Context, _ types.IRequest, _ *router.ServiceContext, service string, _ *types.TargetInfo) (*smodels.MenuServiceSnapshot, error) {
			call++
			return &smodels.MenuServiceSnapshot{Name: service, Routers: []smodels.MenuRouterSnapshot{{
				Path: "/api/manage/users/usermanage/search", InstanceName: fmt.Sprintf("UserManage%d", call),
			}}}, nil
		},
	)
	require.ErrorContains(t, err, "snapshots differ")
}

func TestDiscoverMenuServiceSnapshotsChecksRemoteReplicaOfLocalService(t *testing.T) {
	provider := newMenuDiscoveryProvider(t, "exchange", "exchange")
	source := &router.ServiceContext{
		Service: &types.Service{Name: "exchange"}, ServiceInstanceID: "exchange-1",
		ClusterProvider: provider,
	}
	calls := 0
	_, err := discoverMenuServiceSnapshotsFrom(
		&menuDiscoveryRequest{source: source},
		map[string]*router.ServiceContext{"exchange": source},
		func(_ context.Context, _ types.IRequest, _ *router.ServiceContext, service string, _ *types.TargetInfo) (*smodels.MenuServiceSnapshot, error) {
			calls++
			return &smodels.MenuServiceSnapshot{Name: service, Routers: []smodels.MenuRouterSnapshot{{
				Path: "/api/manage/exchange/ordermanage/search", InstanceName: "OrderManage",
			}}}, nil
		},
	)
	require.ErrorContains(t, err, "snapshots differ")
	require.Equal(t, 1, calls)
}

func TestDiscoverMenuServiceSnapshotsFailsClosedWhenRemoteServiceUnavailable(t *testing.T) {
	provider := newMenuDiscoveryProvider(t, "exchange", "users")
	req := &menuDiscoveryRequest{
		source: &router.ServiceContext{
			Service: &types.Service{Name: "exchange"}, ServiceInstanceID: "exchange-1",
			ClusterProvider: provider,
		},
		remote:    map[string]*smodels.MenuServiceSnapshot{},
		remoteErr: map[string]error{"users": errors.New("users unavailable")},
	}

	_, err := discoverMenuServiceSnapshotsFrom(req, map[string]*router.ServiceContext{"exchange": req.source}, req.querySnapshot)
	require.ErrorContains(t, err, "users")
	require.ErrorContains(t, err, "unavailable")
}

func TestBuildMenuModelsForServiceGroupsRemoteManageOperations(t *testing.T) {
	snapshot := &smodels.MenuServiceSnapshot{
		Name: "users",
		Routers: []smodels.MenuRouterSnapshot{
			{Path: "/api/manage/users/usermanage/search", InstanceName: "UserManage", Title: "用户管理", TitleEN: "Users"},
			{Path: "/api/manage/users/usermanage/edit", InstanceName: "UserManage", Title: "用户管理", TitleEN: "Users"},
		},
	}

	items := buildMenuModelsForService(snapshot, 42)
	require.Len(t, items, 1)
	require.Equal(t, "UserManage", items[0].Name)
	require.Equal(t, "用户管理", items[0].Title)
	require.Equal(t, "Users", items[0].TitleEN)
	require.Equal(t, "/api/manage/users/usermanage", items[0].Url)
	require.Equal(t, uint(42), items[0].DirectoryModelID)
	require.Len(t, items[0].Permissions, 2)
	require.Equal(t, "search", items[0].Permissions[0].Name)
	require.Equal(t, "edit", items[0].Permissions[1].Name)
}
