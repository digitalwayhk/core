// 本文件校验同服务WS中继边界；普通路由回到已注册处理器，归属路由保留内建中继。
package router

import (
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/types"
)

func (sc *ServiceContext) handleOwnedWebSocketRelay(ctx context.Context, payload *types.PayLoad) (bool, []byte, error) {
	if payload == nil || !isWebSocketRelayPath(payload.TargetPath) {
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
	}
	// 归属引用是可选能力；通过相同信任与节点校验后，普通WS继续正式路由分发。
	if _, ok := info.GetInstance().(types.IWebSocketOwnedReference); !ok {
		return false, nil, nil
	}
	if payload.TargetPath == "/api/servermanage/ws/subscription" {
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

func isWebSocketRelayPath(path string) bool {
	return path == "/api/servermanage/ws/notice" || path == "/api/servermanage/ws/subscription"
}
