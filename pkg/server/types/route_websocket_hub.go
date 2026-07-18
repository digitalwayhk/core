package types

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/digitalwayhk/core/pkg/server/event"
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

// RouteWebSocketHub 保存一个 ServiceContext 内面向外部客户端的 WebSocket 订阅。
// 完整 hash 是订阅隔离边界，分片只用于降低锁竞争，不能替代 hash 分组。
// 内部服务不通过本 Hub 建立 WebSocket 连接；跨节点通知只转发给拥有
// 外部订阅者的节点，节点间传输仍由 EventBridge/MQ/Transport 承担。
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
	authChangeCancel   func()
	authFailureCancel  func()
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
		hub.authChangeCancel, _ = events.Subscribe(CasdoorIdentityChangedEventType, hub.handleAuthIdentityChanged)
		hub.authFailureCancel, _ = events.Subscribe(CasdoorAuthorityUnavailableEventType, hub.handleAuthAuthorityUnavailable)
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
	if info == nil || router == nil {
		return 0
	}
	if h == nil || h.closed.Load() || client == nil || req == nil {
		info.releaseSubscription(router)
		return 0
	}
	if info.GetServiceName() != h.service {
		info.releaseSubscription(router)
		return 0
	}
	authenticated := info.GetAuth() || info.GetPathType() == PrivateType
	leaseIdentity := WebSocketAuthIdentity{}
	if authenticated {
		identity, ok := router.(IWebSocketUserIdentity)
		if !ok {
			info.releaseSubscription(router)
			return 0
		}
		authRequest, ok := req.(IWebSocketAuthRequest)
		if !ok {
			info.releaseSubscription(router)
			return 0
		}
		leaseIdentity, ok = authRequest.GetWebSocketAuthIdentity()
		if !ok || leaseIdentity.ServiceName != h.service || leaseIdentity.AuthType != AuthTypeUser || strings.TrimSpace(leaseIdentity.UID) == "" {
			info.releaseSubscription(router)
			return 0
		}
		identity.SetUserID(leaseIdentity.UID, leaseIdentity.Username)
	}
	hash := getApiHash(router)
	state := h.routeState(info)
	shard := state.shard(hash)
	for {
		if h.closed.Load() {
			info.releaseSubscription(router)
			return 0
		}
		shard.mu.Lock()
		subscription := shard.subscriptions[hash]
		created := subscription == nil
		if created {
			subscription = &routeWebSocketSubscription{
				router:         router,
				clients:        make(map[IWebSocket]routeWebSocketClientLease),
				activationDone: make(chan struct{}),
				activating:     true,
			}
			shard.subscriptions[hash] = subscription
		}
		if !created && subscription.activating {
			done := subscription.activationDone
			shard.mu.Unlock()
			select {
			case <-done:
				if subscription.activationErr != nil {
					info.releaseSubscription(router)
					return 0
				}
				continue
			case <-h.ctx.Done():
				info.releaseSubscription(router)
				return 0
			}
		}
		if existing, exists := subscription.clients[client]; exists {
			if !clientLeaseContainsRouter(existing, router) {
				existing.additional = append(existing.additional, router)
				subscription.clients[client] = existing
			}
			shard.mu.Unlock()
			return hash
		}
		subscription.clients[client] = routeWebSocketClientLease{router: router, request: req, identity: leaseIdentity}
		shard.mu.Unlock()

		if created {
			err := h.publishSubscription(info, hash, true)
			if err == nil && h.closed.Load() {
				err = context.Canceled
			}
			shard.mu.Lock()
			subscription.activationErr = err
			subscription.activating = false
			if err != nil {
				delete(subscription.clients, client)
				delete(shard.subscriptions, hash)
			}
			close(subscription.activationDone)
			shard.mu.Unlock()
			if err != nil {
				info.releaseSubscription(router)
				return 0
			}
			callWebSocketRegister(router, client, req)
		}
		h.stats.activeClients.Add(1)
		info.recordWebSocketConnect(hash)
		return hash
	}
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

retry:
	shard.mu.Lock()
	subscription := shard.subscriptions[hash]
	if subscription == nil {
		shard.mu.Unlock()
		return
	}
	if subscription.activating {
		done := subscription.activationDone
		shard.mu.Unlock()
		select {
		case <-done:
			goto retry
		case <-h.ctx.Done():
			return
		}
	}
	lease, exists := subscription.clients[client]
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

	h.stats.activeClients.Add(-1)
	info.recordWebSocketDisconnect(hash)
	if remaining == 0 {
		_ = h.publishSubscription(info, hash, false)
		callWebSocketUnregister(router, client, lease.request)
	}
	if !sameRouterLease(lease.router, router) {
		info.releaseSubscription(lease.router)
	}
	for _, additional := range lease.additional {
		info.releaseSubscription(additional)
	}
	if remaining == 0 {
		info.releaseSubscription(router)
	}
}

func sameRouterLease(left, right IRouter) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	leftType := reflect.TypeOf(left)
	if leftType != reflect.TypeOf(right) || !leftType.Comparable() {
		return false
	}
	return left == right
}

func clientLeaseContainsRouter(lease routeWebSocketClientLease, router IRouter) bool {
	if sameRouterLease(lease.router, router) {
		return true
	}
	for _, additional := range lease.additional {
		if sameRouterLease(additional, router) {
			return true
		}
	}
	return false
}

func (h *RouteWebSocketHub) publishSubscription(info *RouterInfo, hash uint64, active bool) error {
	if h.events == nil {
		return nil
	}
	path := info.GetPath()
	payload := routeWebSocketSubscriptionEvent{
		Service: h.service,
		Route:   path,
		Hash:    hash,
		Active:  active,
	}
	env := event.NewEnvelope(h.service, routeWebSocketSubscriptionEventType(h.service), nil)
	env.Subject = path
	env.ShardKey = h.service + ":" + path + ":" + strconv.FormatUint(hash, 10)
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
