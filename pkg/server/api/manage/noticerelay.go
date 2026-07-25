package manage

import (
	"encoding/json"
	"net/http"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// ---- ForwardNotice (relay endpoint) ----

// ForwardNotice receives a WebSocket notice forwarded from a peer node and
// dispatches it to local subscribers for the same routePath+hash.
type ForwardNotice struct {
	RoutePath string          `json:"route_path"`
	Hash      uint64          `json:"hash"`
	Message   json.RawMessage `json:"message"`
}

func (f *ForwardNotice) Parse(req types.IRequest) error    { return req.Bind(f) }
func (f *ForwardNotice) Validation(_ types.IRequest) error { return nil }

func (f *ForwardNotice) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	if sc == nil {
		return map[string]bool{"ok": true}, nil
	}

	// Find the RouterInfo for the given route path.
	ri := sc.Router.GetRouter(f.RoutePath)
	if ri == nil {
		return map[string]bool{"ok": true}, nil
	}

	// Decode the raw message into a generic value and dispatch locally.
	var msg interface{}
	if err := json.Unmarshal(f.Message, &msg); err != nil {
		return nil, err
	}
	ri.ExecuteLocalNotice(f.Hash, msg)
	return map[string]bool{"ok": true}, nil
}

func (f *ForwardNotice) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(f,
		router.WithMethod(http.MethodPost),
		router.WithPath("/api/servermanage/ws/notice"),
		router.WithAuth(false), // peer-to-peer, authenticated by network ACL
	)
}

// ---- SyncSubscription ----

// SyncSubscription receives peer subscription change notifications.
type SyncSubscription struct {
	RoutePath string `json:"route_path"`
	Hash      uint64 `json:"hash"`
	NodeID    string `json:"node_id"`
	Active    bool   `json:"active"`
}

func (s *SyncSubscription) Parse(req types.IRequest) error    { return req.Bind(s) }
func (s *SyncSubscription) Validation(_ types.IRequest) error { return nil }

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

func (s *SyncSubscription) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(s,
		router.WithMethod(http.MethodPost),
		router.WithPath("/api/servermanage/ws/subscription"),
		router.WithAuth(false),
	)
}
