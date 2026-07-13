package types

// RegisterWebSocketClient 将兼容入口委托给所属 ServiceContext 的 RouteWebSocketHub。
func (own *RouterInfo) RegisterWebSocketClient(router IRouter, client IWebSocket, req IRequest) uint64 {
	if own == nil {
		return 0
	}
	own.RLock()
	hub := own.webSocketHub
	own.RUnlock()
	if hub == nil {
		return 0
	}
	return hub.Register(own, router, client, req)
}

func (own *RouterInfo) UnRegisterWebSocketHash(hash uint64, client IWebSocket) {
	if own == nil {
		return
	}
	own.RLock()
	hub := own.webSocketHub
	own.RUnlock()
	if hub != nil {
		hub.Unregister(own, hash, client)
	}
}

func (own *RouterInfo) NoticeWebSocket(message interface{}) {
	if own == nil {
		return
	}
	own.RLock()
	hub := own.webSocketHub
	own.RUnlock()
	if hub != nil {
		hub.Notice(own, message)
	}
}

func (own *RouterInfo) CleanupDeadConnections() {
	if own == nil {
		return
	}
	own.RLock()
	hub := own.webSocketHub
	own.RUnlock()
	if hub != nil {
		hub.CleanupDeadConnections(own)
	}
}

func (own *RouterInfo) GetActiveClientCount() int {
	if own == nil {
		return 0
	}
	own.RLock()
	hub := own.webSocketHub
	own.RUnlock()
	if hub == nil {
		return 0
	}
	return hub.ActiveClientCount(own)
}

// ExecuteLocalNotice 只投递到本服务，不再次外发，避免跨节点通知回环。
func (own *RouterInfo) ExecuteLocalNotice(hash uint64, message interface{}) {
	if own == nil {
		return
	}
	own.RLock()
	hub := own.webSocketHub
	own.RUnlock()
	if hub != nil {
		hub.ExecuteLocalNotice(own, hash, message)
	}
}

func (own *RouterInfo) GetSubscribedHashes() []uint64 {
	if own == nil {
		return nil
	}
	own.RLock()
	hub := own.webSocketHub
	own.RUnlock()
	if hub == nil {
		return nil
	}
	return hub.SubscribedHashes(own)
}

// sendToHashClients 仅供旧 WebSocketNotificationSystem 兼容路径使用。
func (own *RouterInfo) sendToHashClients(hash uint64, _ interface{}, data interface{}) {
	if own == nil {
		return
	}
	own.RLock()
	hub := own.webSocketHub
	own.RUnlock()
	if hub == nil {
		return
	}
	state := hub.getRouteState(own)
	if state == nil {
		return
	}
	shard := state.shard(hash)
	shard.mu.RLock()
	subscription := shard.subscriptions[hash]
	clients := make([]IWebSocket, 0)
	if subscription != nil {
		clients = make([]IWebSocket, 0, len(subscription.clients))
		for client := range subscription.clients {
			if client != nil && !client.IsClosed() {
				clients = append(clients, client)
			}
		}
	}
	shard.mu.RUnlock()
	hub.sendPrepared(own, hash, clients, data)
}
