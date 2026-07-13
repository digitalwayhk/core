package types

import "sync"

const routeWebSocketShardCount = 128

type routeWebSocketShard struct {
	mu            sync.RWMutex
	subscriptions map[uint64]*routeWebSocketSubscription
}

type routeWebSocketSubscription struct {
	router  IRouter
	clients map[IWebSocket]IRequest
}

type routeWebSocketState struct {
	info   *RouterInfo
	shards [routeWebSocketShardCount]*routeWebSocketShard
}

func newRouteWebSocketState(info *RouterInfo) *routeWebSocketState {
	state := &routeWebSocketState{info: info}
	for index := range state.shards {
		state.shards[index] = &routeWebSocketShard{
			subscriptions: make(map[uint64]*routeWebSocketSubscription),
		}
	}
	return state
}

func (s *routeWebSocketState) shard(hash uint64) *routeWebSocketShard {
	return s.shards[hash%routeWebSocketShardCount]
}
