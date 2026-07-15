package types

import (
	"encoding/json"

	"github.com/digitalwayhk/core/pkg/server/event"
)

type webSocketRevocationTarget struct {
	info   *RouterInfo
	hash   uint64
	client IWebSocket
}

func (h *RouteWebSocketHub) handleAuthIdentityChanged(envelope *event.Envelope) {
	value := CasdoorEvent{}
	if h == nil || envelope == nil || json.Unmarshal(envelope.Data, &value) != nil || value.ServiceName != h.service {
		return
	}
	// EventBridge 控制处理器内不得同步退订；退订会再次发布订阅控制事件，
	// 若两个事件落在同一分片会形成重入等待。身份撤销先完成可靠投递，
	// 再在独立执行单元中关闭本地租约。
	go h.RevokeIdentity(value)
}

func (h *RouteWebSocketHub) handleAuthAuthorityUnavailable(envelope *event.Envelope) {
	if h == nil || envelope == nil || envelope.Source != h.service {
		return
	}
	go h.CloseCasdoorSessions()
}

// RevokeIdentity 移除并关闭世代落后或已阻断的 Casdoor WebSocket 租约。
func (h *RouteWebSocketHub) RevokeIdentity(value CasdoorEvent) {
	if h == nil || value.ServiceName != h.service || value.Provider != AuthProviderCasdoor {
		return
	}
	h.revokeWebSocketLeases(func(identity WebSocketAuthIdentity) bool {
		return identity.Provider == AuthProviderCasdoor &&
			identity.AuthType == value.AuthType &&
			identity.ProviderSubject == value.ProviderSubject &&
			(value.Blocked || value.Generation > identity.Generation)
	})
}

// CloseCasdoorSessions 在共享撤销权威不可用时关闭本服务全部 Casdoor 认证连接。
// 非 Casdoor 和未认证的公开连接不受影响。
func (h *RouteWebSocketHub) CloseCasdoorSessions() {
	if h == nil {
		return
	}
	h.revokeWebSocketLeases(func(identity WebSocketAuthIdentity) bool {
		return identity.Provider == AuthProviderCasdoor
	})
}

func (h *RouteWebSocketHub) revokeWebSocketLeases(match func(WebSocketAuthIdentity) bool) {
	if h == nil || match == nil || h.closed.Load() {
		return
	}
	targets := make([]webSocketRevocationTarget, 0)
	h.routesMu.RLock()
	for info, state := range h.routes {
		for _, shard := range state.shards {
			shard.mu.RLock()
			for hash, subscription := range shard.subscriptions {
				for client, lease := range subscription.clients {
					if match(lease.identity) {
						targets = append(targets, webSocketRevocationTarget{info: info, hash: hash, client: client})
					}
				}
			}
			shard.mu.RUnlock()
		}
	}
	h.routesMu.RUnlock()

	closed := make(map[IWebSocket]struct{}, len(targets))
	for _, target := range targets {
		h.Unregister(target.info, target.hash, target.client)
		closed[target.client] = struct{}{}
	}
	for client := range closed {
		if closer, ok := client.(IWebSocketCloser); ok {
			_ = closer.Close()
		}
	}
}
