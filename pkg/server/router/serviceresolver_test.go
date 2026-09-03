package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/transport"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type resolverTestTransport struct {
	targetAddress  string
	targetEndpoint string
	targets        []string
}

func (*resolverTestTransport) Name() string                                          { return "resolver-test" }
func (*resolverTestTransport) Start(context.Context) error                           { return nil }
func (*resolverTestTransport) Stop(context.Context) error                            { return nil }
func (*resolverTestTransport) Supports(context.Context, *types.PayLoad, string) bool { return true }
func (*resolverTestTransport) Health(context.Context, string) error                  { return nil }
func (t *resolverTestTransport) Send(_ context.Context, payload *types.PayLoad, target string) ([]byte, error) {
	t.targetAddress = payload.TargetAddress
	t.targetEndpoint = target
	t.targets = append(t.targets, payload.TargetAddress)
	return json.Marshal(&Response{Success: true, Data: "remote"})
}

type resolverTestSelector struct{ transport *resolverTestTransport }

func (s *resolverTestSelector) Select(_ context.Context, _ *types.PayLoad, endpoints transport.TransportEndpoints) (transport.Selection, error) {
	return transport.Selection{Transport: s.transport, Endpoint: endpoints.HTTP}, nil
}

type resolverTestAPI struct{ info *types.RouterInfo }

var _ types.IRequestKeyedServiceCaller = (*Request)(nil)

func (*resolverTestAPI) Parse(types.IRequest) error             { return nil }
func (*resolverTestAPI) Validation(types.IRequest) error        { return nil }
func (*resolverTestAPI) Do(types.IRequest) (interface{}, error) { return nil, nil }
func (a *resolverTestAPI) RouterInfo() *types.RouterInfo        { return a.info }

func newResolverTestAPI(service string) *resolverTestAPI {
	api := &resolverTestAPI{}
	api.info = &types.RouterInfo{
		Path: "/api/servermanage/queryrouters", ServiceName: service,
		PathType: types.ServerManagerType, Method: http.MethodPost,
		StructName: "QueryRouters", InstanceName: "QueryRouters",
	}
	api.info.SetInstance(api)
	return api
}

func TestServiceResolverPrefersLocalContext(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	local := &ServiceContext{
		Service: &types.Service{Name: "orders"},
		Config:  config.NewServiceDefaultConfig("orders", 8080),
	}
	resolver := NewServiceResolver(provider, func(serviceName string) *ServiceContext {
		if serviceName == "orders" {
			return local
		}
		return nil
	})
	defer resolver.Close()

	target, err := resolver.Resolve(context.Background(), "orders")
	require.NoError(t, err)
	assert.Same(t, local, target.Local)
	assert.Equal(t, "orders", target.Info.TargetService)
}

func TestServiceResolverRoundRobinsRunningNodes(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	ctx := context.Background()
	for i := 1; i <= 2; i++ {
		require.NoError(t, provider.Register(ctx, &cluster.NodeInfo{
			ID: fmt.Sprintf("orders-%d", i), ServiceName: "orders",
			DataCenterID: 1, MachineID: int64(i),
			Address: fmt.Sprintf("order-%d", i), Port: 8080,
		}))
	}
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()

	first, err := resolver.Resolve(ctx, "orders")
	require.NoError(t, err)
	second, err := resolver.Resolve(ctx, "orders")
	require.NoError(t, err)

	assert.NotEqual(t, first.NodeID, second.NodeID)
	assert.ElementsMatch(t, []string{"order-1", "order-2"}, []string{first.Info.TargetAddress, second.Info.TargetAddress})
}

// 同一市场 key 的同步调用必须稳定落到同一服务实例。
func TestServiceResolverResolveWithKeyPinsCallsToOneNode(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	ctx := context.Background()
	for i := 1; i <= 2; i++ {
		require.NoError(t, provider.Register(ctx, &cluster.NodeInfo{
			ID: fmt.Sprintf("positions-%d", i), ServiceName: "positions",
			DataCenterID: 1, MachineID: int64(i),
			Address: fmt.Sprintf("position-%d", i), Port: 8080,
		}))
	}
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()

	first, err := resolver.ResolveWithKey(ctx, "positions", "market:BTCUSDT")
	require.NoError(t, err)
	for range 20 {
		resolved, err := resolver.ResolveWithKey(ctx, "positions", "market:BTCUSDT")
		require.NoError(t, err)
		assert.Equal(t, first.NodeID, resolved.NodeID)
	}
}

// 显式 keyed 调用缺少 key 时必须失败，不能静默退回轮询。
func TestServiceResolverResolveWithKeyRejectsEmptyKey(t *testing.T) {
	resolver := NewServiceResolver(nil, func(string) *ServiceContext { return nil })
	defer resolver.Close()

	_, err := resolver.ResolveWithKey(context.Background(), "positions", " ")
	require.Error(t, err)
}

func TestServiceResolverFailsClosedWithoutHealthyNode(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()

	_, err := resolver.Resolve(context.Background(), "orders")
	require.ErrorIs(t, err, ErrTargetServiceUnavailable)
}

func TestServiceContextCallTargetNodeBypassesResolver(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	require.NoError(t, provider.Register(context.Background(), &cluster.NodeInfo{
		ID: "orders-resolver", ServiceName: "orders", Address: "resolver.internal", Port: 8080,
		Status: cluster.NodeStatusRunning,
	}))
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()
	transport := &resolverTestTransport{}
	sc := &ServiceContext{
		Service:           &types.Service{Name: "exchange"},
		Config:            config.NewServiceDefaultConfig("exchange", 8080),
		ServiceResolver:   resolver,
		TransportSelector: &resolverTestSelector{transport: transport},
	}

	_, err := sc.CallTargetNode(context.Background(), "trace", newResolverTestAPI("orders"), &types.TargetInfo{
		TargetService: "orders", TargetAddress: "exact.internal", TargetPort: 18080, TargetGRPCPort: 19090,
	})
	require.NoError(t, err)
	assert.Equal(t, "exact.internal", transport.targetAddress)
	assert.Equal(t, "http://exact.internal:18080", transport.targetEndpoint)
}

func TestRequestGetTargetServerInfoUsesServiceResolver(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	require.NoError(t, provider.Register(context.Background(), &cluster.NodeInfo{
		ID: "orders-remote", ServiceName: "orders",
		DataCenterID: 1, MachineID: 3,
		Address: "orders.internal", Port: 8080, GRPCPort: 19090,
	}))
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()
	req := &Request{service: &ServiceContext{ServiceResolver: resolver}}

	target := req.GetTargetServerInfo("orders")
	require.NotNil(t, target)
	assert.Equal(t, "orders.internal", target.TargetAddress)
	assert.Equal(t, 19090, target.TargetGRPCPort)
}

func TestResolverReturnsProtocolSpecificEndpoints(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	require.NoError(t, provider.Register(context.Background(), &cluster.NodeInfo{
		ID: "orders-protocol-specific", ServiceName: "orders",
		DataCenterID: 1, MachineID: 9,
		Address: "orders.internal", Port: 8080, GRPCPort: 19090,
	}))
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()

	resolved, err := resolver.Resolve(context.Background(), "orders")
	require.NoError(t, err)
	assert.Equal(t, "orders.internal:19090", resolved.Endpoints.GRPC)
	assert.Equal(t, "http://orders.internal:8080", resolved.Endpoints.HTTP)
}

func TestResolverUsesGRPCPortDecodedFromConsulMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/health/service/orders") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("index") == "1" {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Consul-Index", "1")
		_, _ = fmt.Fprint(w, `[{"Service":{"ID":"orders-node","Service":"orders","Address":"orders.internal","Port":8080,"Meta":{"node_id":"node","extra":"{\"grpc_port\":19090}"}},"Checks":[{"Status":"passing"}]}]`)
	}))
	defer server.Close()
	provider, err := cluster.NewConsulProvider(server.URL)
	require.NoError(t, err)
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil }, "grpc")
	defer resolver.Close()

	resolved, err := resolver.Resolve(context.Background(), "orders")
	require.NoError(t, err)
	assert.Equal(t, "orders.internal:19090", resolved.Endpoints.GRPC)
	assert.Equal(t, 19090, resolved.Info.TargetGRPCPort)
}

func TestResolverDoesNotBorrowHTTPPortWhenGRPCPortIsMissing(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	require.NoError(t, provider.Register(context.Background(), &cluster.NodeInfo{
		ID: "orders-http-only", ServiceName: "orders",
		DataCenterID: 1, MachineID: 10,
		Address: "orders.internal", Port: 8080,
	}))
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()

	resolved, err := resolver.Resolve(context.Background(), "orders")
	require.NoError(t, err)
	assert.Empty(t, resolved.Endpoints.GRPC)
	assert.Equal(t, "http://orders.internal:8080", resolved.Endpoints.HTTP)
}

func TestResolverRejectsNodeWithoutSupportedTransport(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	require.NoError(t, provider.Register(context.Background(), &cluster.NodeInfo{
		ID: "orders-socket-only", ServiceName: "orders",
		DataCenterID: 1, MachineID: 11,
		Address: "orders.internal",
	}))
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()

	_, err := resolver.Resolve(context.Background(), "orders")
	require.ErrorIs(t, err, ErrTargetServiceUnavailable)
}

func TestResolverFiltersMixedNodesByAllowedProtocols(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	ctx := context.Background()
	nodes := []*cluster.NodeInfo{
		{ID: "orders-grpc", ServiceName: "orders", DataCenterID: 1, MachineID: 21, Address: "grpc", GRPCPort: 19090},
		{ID: "orders-http", ServiceName: "orders", DataCenterID: 1, MachineID: 22, Address: "http", Port: 8080},
		{ID: "orders-empty", ServiceName: "orders", DataCenterID: 1, MachineID: 23, Address: "empty"},
	}
	for _, node := range nodes {
		require.NoError(t, provider.Register(ctx, node))
	}

	grpcOnly := NewServiceResolver(provider, func(string) *ServiceContext { return nil }, "grpc")
	defer grpcOnly.Close()
	for range 6 {
		resolved, err := grpcOnly.Resolve(ctx, "orders")
		require.NoError(t, err)
		assert.Equal(t, "orders-grpc", resolved.NodeID)
	}

	grpcHTTP := NewServiceResolver(provider, func(string) *ServiceContext { return nil }, "grpc", "http")
	defer grpcHTTP.Close()
	selected := make(map[string]bool)
	for range 6 {
		resolved, err := grpcHTTP.Resolve(ctx, "orders")
		require.NoError(t, err)
		selected[resolved.NodeID] = true
		assert.NotEqual(t, "orders-empty", resolved.NodeID)
	}
	assert.Equal(t, map[string]bool{"orders-grpc": true, "orders-http": true}, selected)
}

func TestServiceContextRemoteCallUsesDiscovery(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	serviceName := fmt.Sprintf("orders-remote-%d", time.Now().UnixNano())
	require.NoError(t, provider.Register(context.Background(), &cluster.NodeInfo{
		ID: serviceName + "-1", ServiceName: serviceName,
		DataCenterID: 1, MachineID: 4,
		Address: "discovered-orders", Port: 8080,
	}))
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()
	transport := &resolverTestTransport{}
	source := &ServiceContext{
		Service:           &types.Service{Name: "users"},
		ServiceResolver:   resolver,
		TransportSelector: &resolverTestSelector{transport: transport},
		Config:            &config.ServerConfig{},
	}

	response, err := source.CallService(&types.PayLoad{
		TraceID: "trace-remote", SourceService: "users",
		TargetService: serviceName, TargetPath: "/api/orders/query",
		Instance: map[string]interface{}{"userID": "user-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "remote", response.GetData())
	assert.Equal(t, "discovered-orders", transport.targetAddress)
}

// ServiceContext keyed 调用必须把同一市场的连续请求发送到同一实例。
func TestServiceContextCallServiceWithKeyPinsRemoteTarget(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	ctx := context.Background()
	serviceName := fmt.Sprintf("positions-keyed-%d", time.Now().UnixNano())
	for i := 1; i <= 2; i++ {
		require.NoError(t, provider.Register(ctx, &cluster.NodeInfo{
			ID: fmt.Sprintf("%s-%d", serviceName, i), ServiceName: serviceName,
			DataCenterID: 1, MachineID: int64(i),
			Address: fmt.Sprintf("position-%d", i), Port: 8080,
		}))
	}
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()
	transport := &resolverTestTransport{}
	source := &ServiceContext{
		Service:           &types.Service{Name: "trades"},
		ServiceResolver:   resolver,
		TransportSelector: &resolverTestSelector{transport: transport},
		Config:            &config.ServerConfig{},
	}

	for range 20 {
		_, err := source.CallServiceWithKey(&types.PayLoad{
			TraceID: "trace-keyed", TargetService: serviceName,
			TargetPath: "/api/positions/prepareriskplan", Instance: map[string]any{},
		}, "market:BTCUSDT")
		require.NoError(t, err)
	}
	require.Len(t, transport.targets, 20)
	for _, target := range transport.targets[1:] {
		assert.Equal(t, transport.targets[0], target)
	}
}

// Keyed 调用不得污染调用方持有的 payload，避免后续普通调用意外沿用旧 key。
func TestServiceContextCallServiceWithKeyDoesNotMutatePayload(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	ctx := context.Background()
	serviceName := fmt.Sprintf("positions-keyed-payload-%d", time.Now().UnixNano())
	require.NoError(t, provider.Register(ctx, &cluster.NodeInfo{
		ID: serviceName + "-1", ServiceName: serviceName,
		DataCenterID: 1, MachineID: 1, Address: "position-1", Port: 8080,
	}))
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()
	source := &ServiceContext{
		Service:           &types.Service{Name: "users"},
		ServiceResolver:   resolver,
		TransportSelector: &resolverTestSelector{transport: &resolverTestTransport{}},
		Config:            &config.ServerConfig{},
	}
	payload := &types.PayLoad{
		TraceID: "trace-keyed-copy", TargetService: serviceName,
		TargetPath: "/api/positions/prepareriskplan", Instance: map[string]any{},
	}

	_, err := source.CallServiceWithKey(payload, " market:BTCUSDT ")
	require.NoError(t, err)
	assert.Empty(t, payload.ServiceHashKey)
}

type localDispatchRoute struct {
	info *types.RouterInfo
}

func (r *localDispatchRoute) Parse(types.IRequest) error      { return nil }
func (r *localDispatchRoute) Validation(types.IRequest) error { return nil }
func (r *localDispatchRoute) Do(types.IRequest) (interface{}, error) {
	return "target", nil
}
func (r *localDispatchRoute) RouterInfo() *types.RouterInfo { return r.info }

type localCallerRoute struct {
	info  *types.RouterInfo
	calls int
}

func (*localCallerRoute) Parse(types.IRequest) error      { return nil }
func (*localCallerRoute) Validation(types.IRequest) error { return nil }
func (r *localCallerRoute) Do(types.IRequest) (interface{}, error) {
	r.calls++
	return "caller", nil
}
func (r *localCallerRoute) RouterInfo() *types.RouterInfo { return r.info }

type localDispatchService struct {
	name  string
	route types.IRouter
}

func (s *localDispatchService) ServiceName() string      { return s.name }
func (s *localDispatchService) Routers() []types.IRouter { return []types.IRouter{s.route} }

func TestServiceContextLocalCallExecutesRegisteredTargetRouter(t *testing.T) {
	serviceName := fmt.Sprintf("local-dispatch-target-%d", time.Now().UnixNano())
	path := "/api/" + serviceName + "/create"
	targetRoute := &localDispatchRoute{}
	targetRoute.info = &types.RouterInfo{
		Path: path, ServiceName: serviceName, PathType: types.PrivateType,
		Method: "POST",
	}
	targetRoute.info.SetInstance(targetRoute)
	cfg := config.NewServiceDefaultConfig(serviceName, 0)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	target := NewServiceContextWithConfig(&localDispatchService{name: serviceName, route: targetRoute}, cfg)
	t.Cleanup(func() { target.SetRunState(false) })

	callerRoute := &localCallerRoute{info: targetRoute.info}
	source := &ServiceContext{Service: &types.Service{Name: "users"}}
	response, err := source.CallService(&types.PayLoad{
		TraceID: "trace-local", SourceService: "users",
		TargetService: serviceName, TargetPath: path,
		UserId: "user-1", Auth: true, Instance: callerRoute,
	})
	require.NoError(t, err)
	assert.Equal(t, "target", response.GetData())
	assert.Zero(t, callerRoute.calls)
}
