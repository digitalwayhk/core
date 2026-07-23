package types

import "sync"

const routeWebSocketShardCount = 128

type routeWebSocketShard struct {
	mu            sync.RWMutex
	subscriptions map[uint64]*routeWebSocketSubscription
}

type routeWebSocketSubscription struct {
	router         IRouter
	clients        map[IWebSocket]routeWebSocketClientLease
	activationDone chan struct{}
	activationErr  error
	activating     bool
}

// routeWebSocketClientLease 记录一个连接在订阅期间持有的请求对象。
// Router 由 Hub 接管，在客户退订时归还所属 RouterInfo 的对象池。
type routeWebSocketClientLease struct {
	router     IRouter
	request    IRequest
	additional []IRouter
	identity   WebSocketAuthIdentity
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
