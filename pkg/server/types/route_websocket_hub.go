package types

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/zeromicro/go-zero/core/logx"
)

type routeWebSocketSubscriptionEvent struct {
	Service string `json:"service"`
	Route   string `json:"route"`
	Hash    uint64 `json:"hash"`
	Active  bool   `json:"active"`
}

type routeWebSocketNoticeEvent struct {
	Service string `json:"service"`
	Route   string `json:"route"`
	Hash    uint64 `json:"hash"`
	Forward bool   `json:"forward"`
}

// RouteWebSocketHub 保存一个 ServiceContext 内全部路由的 WebSocket 订阅。
// 完整 hash 是订阅隔离边界，分片只用于降低锁竞争，不能替代 hash 分组。
type RouteWebSocketHub struct {
	service string
	events  RouteEventRuntime

	routesMu sync.RWMutex
	routes   map[*RouterInfo]*routeWebSocketState

	ctx       context.Context
	cancel    context.CancelFunc
	closed    atomic.Bool
	closeOnce sync.Once

	subscriptionCancel func()
	noticeCancel       func()
	pendingNotices     sync.Map

	deliveryOnce   sync.Once
	deliveryQueues []chan routeWebSocketDelivery
	deliveryWG     sync.WaitGroup
	stats          routeWebSocketStats
}

func NewRouteWebSocketHub(service string, events RouteEventRuntime) *RouteWebSocketHub {
	ctx, cancel := context.WithCancel(context.Background())
	hub := &RouteWebSocketHub{
		service:        service,
		events:         events,
		routes:         make(map[*RouterInfo]*routeWebSocketState),
		ctx:            ctx,
		cancel:         cancel,
		deliveryQueues: make([]chan routeWebSocketDelivery, 8),
	}
	if events != nil {
		hub.subscriptionCancel, _ = events.Subscribe(routeWebSocketSubscriptionEventType(service), hub.handleSubscriptionEvent)
		hub.noticeCancel, _ = events.Subscribe(routeWebSocketNoticeEventType(service), hub.handleNoticeEvent)
	}
	return hub
}

func routeWebSocketSubscriptionEventType(service string) string {
	return "websocket.subscription:" + service
}

func routeWebSocketNoticeEventType(service string) string {
	return "websocket.notice:" + service
}

func (h *RouteWebSocketHub) Register(info *RouterInfo, router IRouter, client IWebSocket, req IRequest) uint64 {
	if h == nil || h.closed.Load() || info == nil || router == nil || client == nil || req == nil {
		return 0
	}
	if info.ServiceName != h.service {
		return 0
	}
	if info.PathType == PrivateType {
		id, _ := req.GetUser()
		utils.SetPropertyValue(router, "userid", id)
	}
	hash := getApiHash(router)
	state := h.routeState(info)
	shard := state.shard(hash)
	shard.mu.Lock()
	subscription := shard.subscriptions[hash]
	if subscription == nil {
		subscription = &routeWebSocketSubscription{
			router:  router,
			clients: make(map[IWebSocket]IRequest),
		}
		shard.subscriptions[hash] = subscription
	}
	if _, exists := subscription.clients[client]; exists {
		shard.mu.Unlock()
		return hash
	}
	first := len(subscription.clients) == 0
	subscription.clients[client] = req
	count := len(subscription.clients)
	shard.mu.Unlock()

	if first {
		if err := h.publishSubscription(info, hash, true); err != nil {
			shard.mu.Lock()
			delete(subscription.clients, client)
			delete(shard.subscriptions, hash)
			shard.mu.Unlock()
			return 0
		}
		callWebSocketRegister(router, client, req)
	}
	h.updateCompatibilityView(info, hash, router, count)
	h.stats.activeClients.Add(1)
	info.recordWebSocketConnect(hash)
	return hash
}

func (h *RouteWebSocketHub) Unregister(info *RouterInfo, hash uint64, client IWebSocket) {
	if h == nil || info == nil || client == nil {
		return
	}
	state := h.getRouteState(info)
	if state == nil {
		return
	}
	shard := state.shard(hash)
	shard.mu.Lock()
	subscription := shard.subscriptions[hash]
	if subscription == nil {
		shard.mu.Unlock()
		return
	}
	req, exists := subscription.clients[client]
	if !exists {
		shard.mu.Unlock()
		return
	}
	delete(subscription.clients, client)
	remaining := len(subscription.clients)
	router := subscription.router
	if remaining == 0 {
		delete(shard.subscriptions, hash)
	}
	shard.mu.Unlock()

	h.updateCompatibilityView(info, hash, router, remaining)
	h.stats.activeClients.Add(-1)
	info.recordWebSocketDisconnect(hash)
	if remaining == 0 {
		_ = h.publishSubscription(info, hash, false)
		callWebSocketUnregister(router, client, req)
	}
}

func (h *RouteWebSocketHub) publishSubscription(info *RouterInfo, hash uint64, active bool) error {
	if h.events == nil {
		return nil
	}
	payload := routeWebSocketSubscriptionEvent{
		Service: h.service,
		Route:   info.Path,
		Hash:    hash,
		Active:  active,
	}
	env := event.NewEnvelope(h.service, routeWebSocketSubscriptionEventType(h.service), nil)
	env.Subject = info.Path
	env.ShardKey = h.service + ":" + info.Path + ":" + strconv.FormatUint(hash, 10)
	return h.events.Publish(context.Background(), event.PublishRequest{
		Class:    event.ControlDelivery,
		Envelope: env,
		BuildData: func() ([]byte, error) {
			return json.Marshal(payload)
		},
	})
}

func (h *RouteWebSocketHub) handleSubscriptionEvent(env *event.Envelope) {
	payload := routeWebSocketSubscriptionEvent{}
	if env == nil || json.Unmarshal(env.Data, &payload) != nil || payload.Service != h.service {
		return
	}
	if forwarder := GetCrossNodeForwarderForService(h.service); forwarder != nil {
		forwarder.OnSubscriptionChange(payload.Route, payload.Hash, payload.Active)
	}
}

func (h *RouteWebSocketHub) routeState(info *RouterInfo) *routeWebSocketState {
	h.routesMu.Lock()
	state := h.routes[info]
	if state == nil {
		state = newRouteWebSocketState(info)
		h.routes[info] = state
	}
	h.routesMu.Unlock()
	return state
}

func (h *RouteWebSocketHub) getRouteState(info *RouterInfo) *routeWebSocketState {
	h.routesMu.RLock()
	state := h.routes[info]
	h.routesMu.RUnlock()
	return state
}

func (h *RouteWebSocketHub) updateCompatibilityView(info *RouterInfo, hash uint64, router IRouter, count int) {
	info.Lock()
	if info.rArgs == nil {
		info.rArgs = make(map[uint64]IRouter)
	}
	if info.rHashClients == nil {
		info.rHashClients = make(map[uint64]int)
	}
	if count == 0 {
		delete(info.rArgs, hash)
		delete(info.rHashClients, hash)
	} else {
		info.rArgs[hash] = router
		info.rHashClients[hash] = count
	}
	info.Unlock()
}

func callWebSocketRegister(router IRouter, client IWebSocket, req IRequest) {
	callback, ok := router.(IWebSocketRouter)
	if !ok {
		return
	}
	defer func() {
		if recover() != nil {
			logx.Errorw("websocket_register_panicked")
		}
	}()
	callback.RegisterWebSocket(client, req)
}

func callWebSocketUnregister(router IRouter, client IWebSocket, req IRequest) {
	callback, ok := router.(IWebSocketRouter)
	if !ok {
		return
	}
	defer func() {
		if recover() != nil {
			logx.Errorw("websocket_unregister_panicked")
		}
	}()
	callback.UnRegisterWebSocket(client, req)
}
