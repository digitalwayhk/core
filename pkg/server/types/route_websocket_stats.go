package types

import "sync/atomic"

type routeWebSocketStats struct {
	activeClients atomic.Int64
	delivered     atomic.Uint64
	sendFailures  atomic.Uint64
	cleaned       atomic.Uint64
}

type RouteWebSocketStats struct {
	ActiveClients int64
	Delivered     uint64
	SendFailures  uint64
	Cleaned       uint64
}

func (h *RouteWebSocketHub) Stats() RouteWebSocketStats {
	if h == nil {
		return RouteWebSocketStats{}
	}
	return RouteWebSocketStats{
		ActiveClients: h.stats.activeClients.Load(),
		Delivered:     h.stats.delivered.Load(),
		SendFailures:  h.stats.sendFailures.Load(),
		Cleaned:       h.stats.cleaned.Load(),
	}
}

func (h *RouteWebSocketHub) hashClientCounts(info *RouterInfo) map[uint64]int {
	counts := make(map[uint64]int)
	if h == nil || info == nil {
		return counts
	}
	state := h.getRouteState(info)
	if state == nil {
		return counts
	}
	for index := range state.shards {
		shard := state.shards[index]
		shard.mu.RLock()
		for hash, subscription := range shard.subscriptions {
			if subscription != nil && len(subscription.clients) > 0 {
				counts[hash] = len(subscription.clients)
			}
		}
		shard.mu.RUnlock()
	}
	return counts
}
