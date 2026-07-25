// Package userservice 组装 07 普通用户入口服务路由能力。
package userservice

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	privateapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/user-service/api/private"
	publicapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/user-service/api/public"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// Service 是普通用户入口服务。
type Service struct {
	mu      sync.Mutex
	cancels []func()
}

// ServiceName 返回用户服务稳定逻辑名。
func (*Service) ServiceName() string { return contract.UserServiceName }

// Routers 注册用户服务外部 facade 和买家 Private API。
func (*Service) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{
		&publicapi.GetSuppliers{}, &publicapi.GetProducts{}, &publicapi.GetPaymentTypes{},
		&privateapi.AddOrder{}, &privateapi.GetOrders{}, &privateapi.CancelOrder{}, &privateapi.CreatePayment{},
	}
}

// SubscribeRouters 保留旧观察路由兼容入口；内部事件订阅后续统一用 EventBridge。
func (*Service) SubscribeRouters() []*servertypes.ObserveArgs { return nil }

// Start 订阅订单服务发布的可靠事件，用于失效买家订单缓存并通知 WebSocket。
func (s *Service) Start() {
	sc := router.GetContext(contract.UserServiceName)
	if sc == nil {
		panic(fmt.Errorf("用户服务缺失 ServiceContext: %s", contract.UserServiceName))
	}
	s.registerEventSubscriptions(sc)
}

// Stop 释放用户服务启动时注册的事件订阅。
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelEventSubscriptions()
}

// registerEventSubscriptions 注册用户入口服务消费的内部订单事件。
func (s *Service) registerEventSubscriptions(sc *router.ServiceContext) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, subscription := range userEventSubscriptions() {
		cancel, err := sc.SubscribeEvent(subscription)
		if err != nil {
			s.cancelEventSubscriptions()
			panic(err)
		}
		s.cancels = append(s.cancels, cancel)
	}
}

// userEventSubscriptions 描述用户服务需要消费的内部事件集合。
func userEventSubscriptions() []event.Subscription {
	return []event.Subscription{
		{Subject: contract.SubjectOrderChanged, EventType: contract.EventOrderCreated, Reliable: true, Handler: handleUserOrderEvent},
		{Subject: contract.SubjectOrderChanged, EventType: contract.EventOrderStatusChanged, Reliable: true, Handler: handleUserOrderEvent},
		{Subject: contract.SubjectOrderChanged, EventType: contract.EventPaymentChanged, Reliable: true, Handler: handleUserOrderEvent},
	}
}

// handleUserOrderEvent 处理订单变化事件，并只通知订单所属买家的 WebSocket 订阅。
func handleUserOrderEvent(_ context.Context, env *event.Envelope) error {
	payload := &orderdto.OrderChanged{}
	if err := json.Unmarshal(env.Data, payload); err != nil {
		return err
	}
	if payload.SchemaVersion != contract.EventSchemaVersion {
		return fmt.Errorf("不支持的事件 schemaVersion: %d", payload.SchemaVersion)
	}
	if alreadyProcessedOrderEvent(payload.EventID, payload.EventType) {
		return nil
	}
	privateapi.InvalidateOrderCache(payload.UserID)
	(&privateapi.GetOrders{}).RouterInfo().NoticeWebSocket(payload)
	return nil
}

// cancelEventSubscriptions 按注册时保存的取消函数释放订阅资源。
func (s *Service) cancelEventSubscriptions() {
	for _, cancel := range s.cancels {
		cancel()
	}
	s.cancels = nil
}

var processedOrderEvents sync.Map

// alreadyProcessedOrderEvent 在无本地用户库的入口 facade 中提供进程内事件幂等。
func alreadyProcessedOrderEvent(eventID, eventType string) bool {
	_, loaded := processedOrderEvents.LoadOrStore(eventType+":"+eventID, struct{}{})
	return loaded
}
