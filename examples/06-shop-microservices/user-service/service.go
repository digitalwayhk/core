// 本文件组装当前服务的路由、事件订阅、Outbox 和生命周期能力。
package userservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	manageapi "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/api/manage"
	privateapi "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/api/private"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// Service 是买家唯一外部入口，不保存订单或商品权威副本。
type Service struct {
	mu      sync.Mutex
	cancels []func()
}

// ServiceName 返回用户服务的稳定逻辑服务名，供路由、发现和内部调用鉴权使用。
func (*Service) ServiceName() string { return contract.UserServiceName }

// Routers 注册用户服务对外 facade、买家 Private API 和用户资料 Manage API。
func (*Service) Routers() []servertypes.IRouter {
	routers := []servertypes.IRouter{&publicapi.GetSuppliers{}, &publicapi.GetProducts{}, &publicapi.GetPaymentTypes{}, &privateapi.AddOrder{}, &privateapi.GetOrders{}, &privateapi.CancelOrder{}, &privateapi.CreatePayment{}}
	routers = append(routers, manageapi.NewUserManage().Routers()...)
	routers = append(routers, manageapi.NewAddressManage().Routers()...)
	return routers
}

// SubscribeRouters 保留旧观察路由兼容入口；用户服务内部事件统一走 EventBridge。
func (*Service) SubscribeRouters() []*servertypes.ObserveArgs { return nil }

// OnAuth 在 TestToken 登录时幂等建立普通用户资料，平台管理员不落入买家资料表。
func (*Service) OnAuth(_ context.Context, args *servertypes.AuthHookArgs) error {
	if args == nil || strings.TrimSpace(args.UID) == "" {
		return contract.ErrInvalidIdentity
	}
	if args.UID == contract.PlatformAdminUserID {
		return nil
	}
	_, err := models.EnsureUser(args.UID, args.Username)
	return err
}

// Start 订阅供应商、商品、支付类型和订单事件，维护入口 facade 缓存与买家 WebSocket。
func (s *Service) Start() {
	sc := router.GetContext(contract.UserServiceName)
	if sc == nil || sc.ServiceEventBridge == nil {
		return
	}
	s.registerEventSubscriptions(sc)
}

// registerEventSubscriptions 统一注册用户服务的可靠内部订阅，并保存取消函数便于 Stop 清理。
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

// userEventSubscriptions 描述用户服务消费的全部内部事件，不在 Start 中堆匿名订阅。
func userEventSubscriptions() []event.Subscription {
	return []event.Subscription{
		{Subject: contract.SubjectProductChanged, EventType: contract.EventProductChanged, Reliable: true, Handler: handleUserCacheEvent},
		{Subject: contract.SubjectSupplierChanged, EventType: contract.EventSupplierChanged, Reliable: true, Handler: handleUserCacheEvent},
		{Subject: contract.SubjectPaymentTypeChanged, EventType: contract.EventPaymentTypeChanged, Reliable: true, Handler: handleUserCacheEvent},
		{Subject: contract.SubjectOrderCreated, EventType: contract.EventOrderCreated, Reliable: true, Handler: handleUserOrderEvent},
		{Subject: contract.SubjectOrderStatusChanged, EventType: contract.EventOrderStatusChanged, Reliable: true, Handler: handleUserOrderEvent},
		{Subject: contract.SubjectPaymentChanged, EventType: contract.EventPaymentChanged, Reliable: true, Handler: handleUserOrderEvent},
	}
}

// handleUserCacheEvent 消费权威服务变更事件，只失效入口 facade 缓存，不写权威数据。
func handleUserCacheEvent(_ context.Context, env *event.Envelope) error {
	metadata := &eventdto.Metadata{}
	if err := json.Unmarshal(env.Data, metadata); err != nil {
		return err
	}
	if metadata.SchemaVersion != contract.EventSchemaVersion {
		return fmt.Errorf("不支持的事件 schemaVersion: %d", metadata.SchemaVersion)
	}
	return models.ProcessInbox(metadata.TraceID, metadata.EventID, metadata.EventType, func() error {
		switch metadata.EventType {
		case contract.EventSupplierChanged:
			(&publicapi.GetSuppliers{}).RouterInfo().FailureCache(nil)
			(&publicapi.GetProducts{}).RouterInfo().FailureCache(nil)
		case contract.EventProductChanged:
			(&publicapi.GetProducts{}).RouterInfo().FailureCache(nil)
		case contract.EventPaymentTypeChanged:
			(&publicapi.GetPaymentTypes{}).RouterInfo().FailureCache(nil)
		}
		return nil
	})
}

// handleUserOrderEvent 消费订单状态事件，用 Inbox 幂等后失效买家订单缓存并通知 WebSocket。
func handleUserOrderEvent(_ context.Context, env *event.Envelope) error {
	payload := &eventdto.OrderChanged{}
	if err := json.Unmarshal(env.Data, payload); err != nil {
		return err
	}
	if payload.SchemaVersion != contract.EventSchemaVersion {
		return fmt.Errorf("不支持的订单事件 schemaVersion: %d", payload.SchemaVersion)
	}
	return models.ProcessInbox(payload.TraceID, payload.EventID, payload.EventType, func() error {
		privateapi.InvalidateOrderCache(payload.UserID)
		(&privateapi.GetOrders{}).RouterInfo().NoticeWebSocket(payload)
		return nil
	})
}

// Stop 注销用户服务启动时注册的内部事件订阅。
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelEventSubscriptions()
}

// cancelEventSubscriptions 按保存的取消函数释放 EventBridge 订阅资源。
func (s *Service) cancelEventSubscriptions() {
	for _, cancel := range s.cancels {
		cancel()
	}
	s.cancels = nil
}
