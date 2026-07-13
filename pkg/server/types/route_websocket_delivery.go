package types

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/zeromicro/go-zero/core/logx"
)

type routeWebSocketPendingNotice struct {
	info    *RouterInfo
	message interface{}
	forward bool
}

type routeWebSocketDelivery struct {
	info    *RouterInfo
	hash    uint64
	message interface{}
}

func (h *RouteWebSocketHub) Notice(info *RouterInfo, message interface{}) {
	for _, hash := range h.SubscribedHashes(info) {
		h.publishNotice(info, hash, message, true)
	}
}

func (h *RouteWebSocketHub) ExecuteLocalNotice(info *RouterInfo, hash uint64, message interface{}) {
	if h == nil || info == nil || h.closed.Load() {
		return
	}
	h.publishNotice(info, hash, message, false)
}

func (h *RouteWebSocketHub) publishNotice(info *RouterInfo, hash uint64, message interface{}, forward bool) {
	if h.events == nil {
		h.enqueueDelivery(routeWebSocketDelivery{info: info, hash: hash, message: message})
		return
	}
	payload := routeWebSocketNoticeEvent{
		Service: h.service,
		Route:   info.Path,
		Hash:    hash,
		Forward: forward,
	}
	env := event.NewEnvelope(h.service, routeWebSocketNoticeEventType(h.service), nil)
	env.Subject = info.Path
	env.ShardKey = h.service + ":" + info.Path + ":" + strconv.FormatUint(hash, 10)
	h.pendingNotices.Store(env.ID, routeWebSocketPendingNotice{info: info, message: message, forward: forward})
	err := h.events.Publish(context.Background(), event.PublishRequest{
		Class:    event.ControlDelivery,
		Envelope: env,
		BuildData: func() ([]byte, error) {
			return json.Marshal(payload)
		},
	})
	if err != nil {
		h.pendingNotices.Delete(env.ID)
		logx.Errorw("websocket_notice_publish_failed",
			logx.Field("service", h.service),
			logx.Field("route", info.Path),
			logx.Field("hash", hash),
			logx.Field("error", err),
		)
	}
}

func (h *RouteWebSocketHub) handleNoticeEvent(env *event.Envelope) {
	if env == nil {
		return
	}
	value, exists := h.pendingNotices.LoadAndDelete(env.ID)
	if !exists {
		return
	}
	pending := value.(routeWebSocketPendingNotice)
	h.enqueueDelivery(routeWebSocketDelivery{
		info:    pending.info,
		hash:    routeWebSocketHashFromEnvelope(env),
		message: pending.message,
	})
	if pending.forward {
		if forwarder := GetCrossNodeForwarderForService(h.service); forwarder != nil {
			forwarder.ForwardNotice(context.Background(), pending.info.Path, routeWebSocketHashFromEnvelope(env), pending.message)
		}
	}
}

func routeWebSocketHashFromEnvelope(env *event.Envelope) uint64 {
	payload := routeWebSocketNoticeEvent{}
	if json.Unmarshal(env.Data, &payload) != nil {
		return 0
	}
	return payload.Hash
}

func (h *RouteWebSocketHub) enqueueDelivery(delivery routeWebSocketDelivery) {
	if h.closed.Load() {
		return
	}
	h.ensureDeliveryWorkers()
	queue := h.deliveryQueues[delivery.hash%uint64(len(h.deliveryQueues))]
	select {
	case queue <- delivery:
	case <-h.ctx.Done():
	}
}

func (h *RouteWebSocketHub) ensureDeliveryWorkers() {
	h.deliveryOnce.Do(func() {
		for index := range h.deliveryQueues {
			queue := make(chan routeWebSocketDelivery, 256)
			h.deliveryQueues[index] = queue
			h.deliveryWG.Add(1)
			go h.runDeliveryWorker(queue)
		}
	})
}

func (h *RouteWebSocketHub) runDeliveryWorker(queue <-chan routeWebSocketDelivery) {
	defer h.deliveryWG.Done()
	for {
		select {
		case <-h.ctx.Done():
			return
		case delivery := <-queue:
			h.deliver(delivery)
		}
	}
}

func (h *RouteWebSocketHub) deliver(delivery routeWebSocketDelivery) {
	state := h.getRouteState(delivery.info)
	if state == nil {
		return
	}
	shard := state.shard(delivery.hash)
	shard.mu.RLock()
	subscription := shard.subscriptions[delivery.hash]
	if subscription == nil {
		shard.mu.RUnlock()
		return
	}
	router := subscription.router
	clients := make([]IWebSocket, 0, len(subscription.clients))
	for client := range subscription.clients {
		if client != nil && !client.IsClosed() {
			clients = append(clients, client)
		}
	}
	shard.mu.RUnlock()

	noticeRouter, ok := delivery.info.GetInstance().(IWebSocketRouterNotice)
	if !ok {
		return
	}
	accepted, data := callWebSocketFilter(noticeRouter, delivery.message, router)
	if !accepted {
		return
	}
	h.sendPrepared(delivery.info, delivery.hash, clients, data)
}

func (h *RouteWebSocketHub) sendPrepared(info *RouterInfo, hash uint64, clients []IWebSocket, data interface{}) {
	eventName := strconv.FormatUint(hash, 10)
	for _, client := range clients {
		if sendWebSocket(client, eventName, info.Path, data) {
			h.stats.delivered.Add(1)
			info.recordWebSocketMessage(0)
		} else {
			h.stats.sendFailures.Add(1)
			info.recordWebSocketError()
		}
	}
}

func callWebSocketFilter(router IWebSocketRouterNotice, message interface{}, api IRouter) (ok bool, data interface{}) {
	defer func() {
		if recover() != nil {
			ok = false
			data = nil
		}
	}()
	return router.NoticeFiltersRouter(message, api)
}

func sendWebSocket(client IWebSocket, eventName, channel string, data interface{}) (sent bool) {
	defer func() {
		if recover() != nil {
			sent = false
		}
	}()
	client.Send(eventName, channel, data)
	return true
}
