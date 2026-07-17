package userservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	exampleruntime "github.com/digitalwayhk/core/examples/06-shop-microservices/runtime"
	manageapi "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/api/manage"
	privateapi "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/api/private"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/zeromicro/go-zero/core/logx"
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
	s.mu.Lock()
	defer s.mu.Unlock()
	cacheHandler := func(env *event.Envelope) error {
		metadata := &eventdto.Metadata{}
		if err := json.Unmarshal(env.Data, metadata); err != nil {
			return err
		}
		if metadata.SchemaVersion != contract.EventSchemaVersion {
			return fmt.Errorf("不支持的事件 schemaVersion: %d", metadata.SchemaVersion)
		}
		return models.ProcessInbox(metadata.EventID, metadata.EventType, func() error {
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
	for _, eventType := range []string{contract.EventProductChanged, contract.EventSupplierChanged, contract.EventPaymentTypeChanged} {
		if cancel, subscribeErr := sc.ServiceEventBridge.SubscribeControl(eventType, cacheHandler); subscribeErr == nil {
			s.cancels = append(s.cancels, cancel)
		}
	}
	orderHandler := func(env *event.Envelope) error {
		payload := &eventdto.OrderChanged{}
		if err := json.Unmarshal(env.Data, payload); err != nil {
			return err
		}
		if payload.SchemaVersion != contract.EventSchemaVersion {
			return fmt.Errorf("不支持的订单事件 schemaVersion: %d", payload.SchemaVersion)
		}
		return models.ProcessInbox(payload.EventID, payload.EventType, func() error {
			privateapi.InvalidateOrderCache(payload.UserID)
			(&privateapi.GetOrders{}).RouterInfo().NoticeWebSocket(payload)
			return nil
		})
	}
	for _, eventType := range []string{contract.EventOrderCreated, contract.EventOrderStatusChanged, contract.EventPaymentChanged} {
		if cancel, subscribeErr := sc.ServiceEventBridge.SubscribeControl(eventType, orderHandler); subscribeErr == nil {
			s.cancels = append(s.cancels, cancel)
		}
	}
	externalCancels, err := exampleruntime.SubscribeExternalControls(context.Background(), sc.ServiceEventBridge,
		contract.SubjectProductChanged, contract.SubjectSupplierChanged, contract.SubjectPaymentTypeChanged,
		contract.SubjectOrderCreated, contract.SubjectOrderStatusChanged, contract.SubjectPaymentChanged)
	if err != nil {
		logx.Errorw("service_external_control_subscribe_failed", logx.Field("service", contract.UserServiceName), logx.Field("error", err))
		panic(err)
	}
	s.cancels = append(s.cancels, externalCancels...)
}
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.cancels {
		cancel()
	}
	s.cancels = nil
}
