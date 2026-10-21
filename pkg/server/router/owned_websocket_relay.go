// 本文件接通已有跨节点WS Broker的两个内部中继，仅对显式归属路由及同服务可信身份开放。
package router

import (
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/types"
)

func (sc *ServiceContext) handleOwnedWebSocketRelay(ctx context.Context, payload *types.PayLoad) (bool, []byte, error) {
	if payload == nil || (payload.TargetPath != "/api/servermanage/ws/notice" && payload.TargetPath != "/api/servermanage/ws/subscription") {
		return false, nil, nil
	}
	caller, trusted := types.TrustedInternalCallerFromContext(ctx)
	if sc.Service == nil || !trusted || caller != sc.Service.Name || payload.SourceService != caller {
		return true, nil, types.ErrInternalCallerForbidden
	}
	if err := sc.CheckServiceAdmission(); err != nil {
		return true, nil, err
	}
	if len(payload.Data) > 1<<20 {
		return true, nil, ownership.ErrCapacity
	}
	var data struct {
		RoutePath string          `json:"route_path"`
		Hash      uint64          `json:"hash"`
		NodeID    string          `json:"node_id"`
		Active    bool            `json:"active"`
		Message   json.RawMessage `json:"message"`
	}
	if json.Unmarshal(payload.Data, &data) != nil || data.Hash == 0 || sc.Router == nil {
		return true, nil, ownership.ErrUnknown
	}
	info := sc.Router.GetRouter(data.RoutePath)
	if info == nil {
		return true, nil, ownership.ErrUnknown
	}
	if _, ok := info.GetInstance().(types.IWebSocketOwnedReference); !ok {
		return true, nil, ownership.ErrUnsupported
	}
	if payload.TargetPath == "/api/servermanage/ws/subscription" {
		if sc.CrossNodeBroker == nil || sc.ClusterProvider == nil {
			return true, nil, ownership.ErrNotReady
		}
		node, err := sc.ClusterProvider.Get(ctx, data.NodeID)
		if err != nil {
			return true, nil, err
		}
		if node == nil || node.ServiceName != caller {
			return true, nil, types.ErrInternalCallerForbidden
		}
		sc.CrossNodeBroker.UpdatePeerSubscription(data.RoutePath, data.Hash, data.NodeID, data.Active)
	} else {
		var message interface{}
		if json.Unmarshal(data.Message, &message) != nil {
			return true, nil, ownership.ErrUnknown
		}
		info.ExecuteLocalNotice(data.Hash, message)
	}
	return true, []byte(`{"success":true,"data":{"ok":true}}`), nil
}
