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

func (*ownedRelayService) ServiceName() string      { return "relay-fixture" }
func (*ownedRelayService) Routers() []types.IRouter { return []types.IRouter{&ownedRelayRoute{}} }

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
