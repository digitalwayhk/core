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
