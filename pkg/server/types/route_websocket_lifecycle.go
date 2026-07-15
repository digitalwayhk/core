package types

import (
	"context"
	"sort"
)

func (h *RouteWebSocketHub) CleanupDeadConnections(info *RouterInfo) {
	state := h.getRouteState(info)
	if state == nil {
		return
	}
	type deadConnection struct {
		hash   uint64
		client IWebSocket
	}
	dead := make([]deadConnection, 0)
	for _, shard := range state.shards {
		shard.mu.RLock()
		for hash, subscription := range shard.subscriptions {
			for client := range subscription.clients {
				if client == nil || client.IsClosed() {
					dead = append(dead, deadConnection{hash: hash, client: client})
				}
			}
		}
		shard.mu.RUnlock()
	}
	for _, connection := range dead {
		h.Unregister(info, connection.hash, connection.client)
	}
	if len(dead) > 0 {
		h.stats.cleaned.Add(uint64(len(dead)))
		info.recordDeadConnectionsCleaned(len(dead))
	}
}

func (h *RouteWebSocketHub) ActiveClientCount(info *RouterInfo) int {
	state := h.getRouteState(info)
	if state == nil {
		return 0
	}
	count := 0
	for _, shard := range state.shards {
		shard.mu.RLock()
		for _, subscription := range shard.subscriptions {
			for client := range subscription.clients {
				if client != nil && !client.IsClosed() {
					count++
				}
			}
		}
		shard.mu.RUnlock()
	}
	return count
}

func (h *RouteWebSocketHub) SubscribedHashes(info *RouterInfo) []uint64 {
	state := h.getRouteState(info)
	if state == nil {
		return nil
	}
	hashes := make([]uint64, 0)
	for _, shard := range state.shards {
		shard.mu.RLock()
		for hash := range shard.subscriptions {
			hashes = append(hashes, hash)
		}
		shard.mu.RUnlock()
	}
	sort.Slice(hashes, func(i, j int) bool { return hashes[i] < hashes[j] })
	return hashes
}

func (h *RouteWebSocketHub) Routers(info *RouterInfo) []IRouter {
	state := h.getRouteState(info)
	if state == nil {
		return nil
	}
	routers := make([]IRouter, 0)
	for _, shard := range state.shards {
		shard.mu.RLock()
		for _, subscription := range shard.subscriptions {
			routers = append(routers, subscription.router)
		}
		shard.mu.RUnlock()
	}
	return routers
}

func (h *RouteWebSocketHub) RemoveRoute(info *RouterInfo) {
	if h == nil || info == nil {
		return
	}
	for _, hash := range h.SubscribedHashes(info) {
		state := h.getRouteState(info)
		if state == nil {
			continue
		}
		shard := state.shard(hash)
		shard.mu.RLock()
		subscription := shard.subscriptions[hash]
		if subscription == nil {
			shard.mu.RUnlock()
			continue
		}
		clients := make([]IWebSocket, 0, len(subscription.clients))
		for client := range subscription.clients {
			clients = append(clients, client)
		}
		shard.mu.RUnlock()
		for _, client := range clients {
			h.Unregister(info, hash, client)
		}
	}
	h.routesMu.Lock()
	delete(h.routes, info)
	h.routesMu.Unlock()
}

func (h *RouteWebSocketHub) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.closeOnce.Do(func() {
		h.closed.Store(true)
		h.routesMu.RLock()
		infos := make([]*RouterInfo, 0, len(h.routes))
		for info := range h.routes {
			infos = append(infos, info)
		}
		h.routesMu.RUnlock()
		for _, info := range infos {
			h.RemoveRoute(info)
		}
		if h.subscriptionCancel != nil {
			h.subscriptionCancel()
		}
		if h.noticeCancel != nil {
			h.noticeCancel()
		}
		if h.authChangeCancel != nil {
			h.authChangeCancel()
		}
		if h.authFailureCancel != nil {
			h.authFailureCancel()
		}
		h.cancel()
	})
	done := make(chan struct{})
	go func() {
		h.deliveryWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
