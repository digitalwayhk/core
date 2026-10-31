// 本文件复用正式跨节点WS中继；只允许经框架验证的同服务调用方执行。
package manage

import (
	"encoding/json"
	"net/http"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// ForwardNotice 将同服务对等节点的通知投递给本地相同routePath/hash的订阅者。
type ForwardNotice struct {
	RoutePath string          `json:"route_path"`
	Hash      uint64          `json:"hash"`
	Message   json.RawMessage `json:"message"`
}

// Parse 绑定既有中继JSON协议。
func (f *ForwardNotice) Parse(req types.IRequest) error { return req.Bind(f) }

// Validation 拒绝未经框架验证的同服务调用方。
func (f *ForwardNotice) Validation(req types.IRequest) error {
	return validateWebSocketRelayCaller(req)
}

// Do 通过现有服务作用域中继机制执行。
func (f *ForwardNotice) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	if sc == nil {
		return map[string]bool{"ok": true}, nil
	}

	// 查找目标服务已经注册的通知路由。
	ri := sc.Router.GetRouter(f.RoutePath)
	if ri == nil {
		return map[string]bool{"ok": true}, nil
	}

	// 还原跨节点JSON消息并经现有通知系统本地投递。
	var msg interface{}
	if err := json.Unmarshal(f.Message, &msg); err != nil {
		return nil, err
	}
	ri.ExecuteLocalNotice(f.Hash, msg)
	return map[string]bool{"ok": true}, nil
}

// RouterInfo 保留原精确路径，不用用户或管理Token替代内部身份。
func (f *ForwardNotice) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(f,
		router.WithMethod(http.MethodPost),
		router.WithPath("/api/servermanage/ws/notice"),
		router.WithAuth(false), // 内部服务身份由mTLS及Validation校验，不复用用户或管理Token。
	)
}

// SyncSubscription 更新同服务对等节点的真实订阅索引。
type SyncSubscription struct {
	RoutePath string `json:"route_path"`
	Hash      uint64 `json:"hash"`
	NodeID    string `json:"node_id"`
	Active    bool   `json:"active"`
}

// Parse 绑定既有中继JSON协议。
func (s *SyncSubscription) Parse(req types.IRequest) error { return req.Bind(s) }

// Validation 拒绝未经框架验证的同服务调用方。
func (s *SyncSubscription) Validation(req types.IRequest) error {
	return validateWebSocketRelayCaller(req)
}

// Do 通过现有服务作用域中继机制执行。
func (s *SyncSubscription) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	if sc == nil {
		return map[string]bool{"ok": true}, nil
	}

	if broker, ok := sc.ClusterProvider.(interface {
		UpdatePeerSubscription(routePath string, hash uint64, nodeID string, active bool)
	}); ok {
		broker.UpdatePeerSubscription(s.RoutePath, s.Hash, s.NodeID, s.Active)
	} else if cn := types.GetCrossNodeForwarderForService(req.ServiceName()); cn != nil {
		if cnb, ok := cn.(*cluster.CrossNodeNoticeBroker); ok {
			cnb.UpdatePeerSubscription(s.RoutePath, s.Hash, s.NodeID, s.Active)
		}
	}
	return map[string]bool{"ok": true}, nil
}

// RouterInfo 保留原精确路径，不用用户或管理Token替代内部身份。
func (s *SyncSubscription) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(s,
		router.WithMethod(http.MethodPost),
		router.WithPath("/api/servermanage/ws/subscription"),
		router.WithAuth(false),
	)
}

// validateWebSocketRelayCaller 防止HTTP或用户Token绕过HandleInternalPayload的可信服务边界。
func validateWebSocketRelayCaller(req types.IRequest) error {
	if req != nil {
		if source, ok := req.(types.IRequestInternalCaller); ok {
			if caller, trusted := source.TrustedInternalCaller(); trusted && caller == req.ServiceName() {
				return nil
			}
		}
	}
	return types.NewPublicError(types.ErrorKindForbidden, 0, "", types.ErrInternalCallerForbidden)
}
