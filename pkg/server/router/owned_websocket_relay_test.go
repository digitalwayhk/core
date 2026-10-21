// 本文件验证持续归属WS的框架中继仅接受同逻辑服务的可信内部身份，不开放管理认证域。
package router

import (
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/transport"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type ownedRelayRoute struct{}

func (*ownedRelayRoute) Parse(types.IRequest) error             { return nil }
func (*ownedRelayRoute) Validation(types.IRequest) error        { return nil }
func (*ownedRelayRoute) Do(types.IRequest) (interface{}, error) { return nil, nil }
func (r *ownedRelayRoute) RouterInfo() *types.RouterInfo {
	return NewRouterInfoWithOptions(r, "fixture/api/private", "owned-relay", WithServiceName("relay-fixture"), WithPath("/api/relay-fixture/view"))
}
func (*ownedRelayRoute) WebSocketOwnedReference() (string, string, string) {
	return "scope", "A", "view"
}

type ownedRelayService struct{}

func (*ownedRelayService) ServiceName() string { return "relay-fixture" }
func (*ownedRelayService) Routers() []types.IRouter {
	return []types.IRouter{&ownedRelayRoute{}, &ordinaryRelayRoute{}}
}

func TestOwnedWebSocketRelayAcceptsOnlyTrustedSameService(t *testing.T) {
	ctx := context.Background()
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	require.NoError(t, provider.Register(ctx, &cluster.NodeInfo{ID: "peer", ServiceName: "relay-fixture", ServiceInstanceID: "peer-boot", Address: "127.0.0.1", Port: 8081, DataCenterID: 1, MachineID: 1}))
	sc := &ServiceContext{Service: &types.Service{Name: "relay-fixture", Routers: (&ownedRelayService{}).Routers()}, TransportStats: &transport.Stats{}, ClusterProvider: provider, CrossNodeBroker: cluster.NewCrossNodeNoticeBroker(provider, "relay-fixture", "self")}
	sc.Router = NewServiceRouter(sc, &ownedRelayService{})
	payload := &types.PayLoad{SourceService: "relay-fixture", TargetService: "relay-fixture", TargetPath: "/api/servermanage/ws/subscription", Data: json.RawMessage(`{"route_path":"/api/relay-fixture/view","hash":88,"node_id":"peer","active":true}`)}
	_, err := sc.HandleInternalPayload(ctx, payload)
	require.Error(t, err, "自报SourceService不建立可信身份")
	_, err = sc.HandleInternalPayload(types.ContextWithTrustedInternalCaller(ctx, "another-service"), payload)
	require.Error(t, err)
	trusted := types.ContextWithTrustedInternalCaller(ctx, "relay-fixture")
	_, err = sc.HandleInternalPayload(trusted, payload)
	require.NoError(t, err, "正式mTLS边界注入的同服务身份应可同步已声明WS引用")
	require.Equal(t, []uint64{88}, sc.CrossNodeBroker.PeerSubscribedHashes("/api/relay-fixture/view"))
	payload.TargetPath = "/api/servermanage/anything"
	_, err = sc.HandleInternalPayload(trusted, payload)
	require.Error(t, err, "不能把同服务可信身份扩展为管理权限")
	sc.Router.unregisterRouterInfos()
}

// ordinaryRelayRoute 不实现归属接口，普通WS不能被可选归属分支截断。
type ordinaryRelayRoute struct{}

// Parse 不绑定客户端身份。
func (*ordinaryRelayRoute) Parse(types.IRequest) error { return nil }

// Validation 为中继分支单元夹具保留无副作用校验。
func (*ordinaryRelayRoute) Validation(types.IRequest) error { return nil }

// Do 不执行业务，实际正式处理器由独立进程测试覆盖。
func (*ordinaryRelayRoute) Do(types.IRequest) (interface{}, error) { return nil, nil }

// RouterInfo 注册没有可选归属能力的普通Private路由。
func (r *ordinaryRelayRoute) RouterInfo() *types.RouterInfo {
	return NewRouterInfoWithOptions(r, "fixture/api/private", "ordinary-relay", WithServiceName("relay-fixture"), WithPath("/api/relay-fixture/ordinary"))
}

// TestOrdinaryWebSocketRelayFallsThroughAfterGuards 锁定普通路由回退和回退前的可信身份/节点检查；实际mTLS投递由独立进程测试覆盖。
func TestOrdinaryWebSocketRelayFallsThroughAfterGuards(t *testing.T) {
	ctx := context.Background()
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	require.NoError(t, provider.Register(ctx, &cluster.NodeInfo{ID: "peer", ServiceName: "relay-fixture", ServiceInstanceID: "peer-boot", Address: "127.0.0.1", Port: 8081, DataCenterID: 1, MachineID: 1}))
	sc := &ServiceContext{Service: &types.Service{Name: "relay-fixture", Routers: (&ownedRelayService{}).Routers()}, TransportStats: &transport.Stats{}, ClusterProvider: provider, CrossNodeBroker: cluster.NewCrossNodeNoticeBroker(provider, "relay-fixture", "self")}
	sc.Router = NewServiceRouter(sc, &ownedRelayService{})
	defer sc.Router.unregisterRouterInfos()
	for _, path := range []string{"/api/servermanage/ws/subscription", "/api/servermanage/ws/notice"} {
		t.Run(path, func(t *testing.T) {
			payload := &types.PayLoad{SourceService: "relay-fixture", TargetService: "relay-fixture", TargetPath: path, Data: json.RawMessage(`{"route_path":"/api/relay-fixture/ordinary","hash":88,"node_id":"peer","active":true,"message":{}}`)}
			trusted := types.ContextWithTrustedInternalCaller(ctx, "relay-fixture")
			handled, _, err := sc.handleOwnedWebSocketRelay(trusted, payload)
			require.NoError(t, err)
			require.False(t, handled, "交回invokePayload和正式注册处理器，不能伪造成功响应")
			handled, _, err = sc.handleOwnedWebSocketRelay(ctx, payload)
			require.True(t, handled)
			require.ErrorIs(t, err, types.ErrInternalCallerForbidden)
			payload.SourceService = "forged"
			handled, _, err = sc.handleOwnedWebSocketRelay(trusted, payload)
			require.True(t, handled)
			require.ErrorIs(t, err, types.ErrInternalCallerForbidden)
			payload.SourceService = "relay-fixture"
			payload.Data = json.RawMessage(`{"route_path":"/api/relay-fixture/unknown","hash":88,"node_id":"peer"}`)
			handled, _, err = sc.handleOwnedWebSocketRelay(trusted, payload)
			require.True(t, handled)
			require.Error(t, err)
			if path == "/api/servermanage/ws/subscription" {
				payload.Data = json.RawMessage(`{"route_path":"/api/relay-fixture/ordinary","hash":88,"node_id":"unknown"}`)
				handled, _, err = sc.handleOwnedWebSocketRelay(trusted, payload)
				require.True(t, handled)
				require.Error(t, err, "不能在节点验证之前交回正式处理器")
			}
		})
	}
}
