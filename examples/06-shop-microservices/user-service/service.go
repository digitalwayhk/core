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

func (*Service) ServiceName() string { return contract.UserServiceName }
func (*Service) Routers() []servertypes.IRouter {
	routers := []servertypes.IRouter{&publicapi.GetSuppliers{}, &publicapi.GetProducts{}, &publicapi.GetPaymentTypes{}, &privateapi.AddOrder{}, &privateapi.GetOrders{}, &privateapi.CancelOrder{}, &privateapi.CreatePayment{}}
	routers = append(routers, manageapi.NewUserManage().Routers()...)
	routers = append(routers, manageapi.NewAddressManage().Routers()...)
	return routers
}
func (*Service) SubscribeRouters() []*servertypes.ObserveArgs { return nil }
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

func (s *Service) Start() {
	sc := router.GetContext(contract.UserServiceName)
	if sc == nil || sc.ServiceEventBridge == nil {
		return
	}
	s.registerEventSubscriptions(sc)
}

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

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelEventSubscriptions()
}

func (s *Service) cancelEventSubscriptions() {
	for _, cancel := range s.cancels {
		cancel()
	}
	s.cancels = nil
}
