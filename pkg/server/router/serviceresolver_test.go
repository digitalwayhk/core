package router

import (
	"context"
	"encoding/json"
	"fmt"
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
	targetAddress string
}

func (*resolverTestTransport) Name() string                                          { return "resolver-test" }
func (*resolverTestTransport) Start(context.Context) error                           { return nil }
func (*resolverTestTransport) Stop(context.Context) error                            { return nil }
func (*resolverTestTransport) Supports(context.Context, *types.PayLoad, string) bool { return true }
func (*resolverTestTransport) Health(context.Context, string) error                  { return nil }
func (t *resolverTestTransport) Send(_ context.Context, payload *types.PayLoad, _ string) ([]byte, error) {
	t.targetAddress = payload.TargetAddress
	return json.Marshal(&Response{Success: true, Data: "remote"})
}

type resolverTestSelector struct{ transport *resolverTestTransport }

func (s *resolverTestSelector) Select(_ context.Context, _ *types.PayLoad, endpoints transport.TransportEndpoints) (transport.Selection, error) {
	return transport.Selection{Transport: s.transport, Endpoint: endpoints.HTTP}, nil
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
			Address: fmt.Sprintf("order-%d", i), Port: 8080, SocketPort: 18080,
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

func TestServiceResolverFailsClosedWithoutHealthyNode(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()

	_, err := resolver.Resolve(context.Background(), "orders")
	require.ErrorIs(t, err, ErrTargetServiceUnavailable)
}

func TestRequestGetTargetServerInfoUsesServiceResolver(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	require.NoError(t, provider.Register(context.Background(), &cluster.NodeInfo{
		ID: "orders-remote", ServiceName: "orders",
		DataCenterID: 1, MachineID: 3,
		Address: "orders.internal", Port: 8080, SocketPort: 18080, GRPCPort: 19090,
	}))
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()
	req := &Request{service: &ServiceContext{ServiceResolver: resolver}}

	target := req.GetTargetServerInfo("orders")
	require.NotNil(t, target)
	assert.Equal(t, "orders.internal", target.TargetAddress)
	assert.Equal(t, 18080, target.TargetSocketPort)
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

func TestResolverAcceptsSocketOnlyNodeDuringMigration(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	require.NoError(t, provider.Register(context.Background(), &cluster.NodeInfo{
		ID: "orders-socket-only", ServiceName: "orders",
		DataCenterID: 1, MachineID: 11,
		Address: "orders.internal", SocketPort: 18080,
	}))
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()

	resolved, err := resolver.Resolve(context.Background(), "orders")
	require.NoError(t, err)
	assert.Equal(t, "orders.internal:18080", resolved.Endpoints.Socket)
}

func TestServiceContextRemoteCallUsesDiscoveryInsteadOfAttachServices(t *testing.T) {
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	serviceName := fmt.Sprintf("orders-remote-%d", time.Now().UnixNano())
	require.NoError(t, provider.Register(context.Background(), &cluster.NodeInfo{
		ID: serviceName + "-1", ServiceName: serviceName,
		DataCenterID: 1, MachineID: 4,
		Address: "discovered-orders", Port: 8080, SocketPort: 18080,
	}))
	resolver := NewServiceResolver(provider, func(string) *ServiceContext { return nil })
	defer resolver.Close()
	transport := &resolverTestTransport{}
	source := &ServiceContext{
		Service:           &types.Service{Name: "users"},
		ServiceResolver:   resolver,
		TransportSelector: &resolverTestSelector{transport: transport},
		Config: &config.ServerConfig{AttachServices: map[string]*config.AttachAddress{
			serviceName: {Name: serviceName, Address: "legacy-orders", Port: 9999},
		}},
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

func (s *localDispatchService) ServiceName() string                  { return s.name }
func (s *localDispatchService) Routers() []types.IRouter             { return []types.IRouter{s.route} }
func (*localDispatchService) SubscribeRouters() []*types.ObserveArgs { return nil }

func TestServiceContextLocalCallExecutesRegisteredTargetRouter(t *testing.T) {
	serviceName := fmt.Sprintf("local-dispatch-target-%d", time.Now().UnixNano())
	path := "/api/" + serviceName + "/create"
	targetRoute := &localDispatchRoute{}
	targetRoute.info = &types.RouterInfo{
		Path: path, ServiceName: serviceName, PathType: types.PrivateType,
		Method: "POST", Subscriber: make(map[types.ObserveState]map[string]*types.ObserveArgs),
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
