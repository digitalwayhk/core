package userservice

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
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
	return []servertypes.IRouter{&publicapi.GetProducts{}, &privateapi.AddAddress{}, &privateapi.GetAddresses{}, &privateapi.DeleteAddress{}, &privateapi.AddOrder{}, &privateapi.GetOrders{}, &privateapi.DeleteOrder{}}
}
func (*Service) SubscribeRouters() []*servertypes.ObserveArgs { return nil }
func (*Service) OnAuth(_ context.Context, args *servertypes.AuthHookArgs) error {
	if args == nil || strings.TrimSpace(args.UID) == "" {
		return contract.ErrInvalidIdentity
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
	productCancel, err := sc.ServiceEventBridge.SubscribeControl(contract.EventProductChanged, func(env *event.Envelope) error {
		payload := &eventdto.ProductChanged{}
		if err := json.Unmarshal(env.Data, payload); err != nil {
			return err
		}
		return models.ProcessInbox(payload.EventID, payload.EventType, func() error { (&publicapi.GetProducts{}).RouterInfo().FailureCache(nil); return nil })
	})
	if err == nil {
		s.cancels = append(s.cancels, productCancel)
	}
	orderCancel, err := sc.ServiceEventBridge.SubscribeControl(contract.EventOrderChanged, func(env *event.Envelope) error {
		payload := &eventdto.OrderChanged{}
		if err := json.Unmarshal(env.Data, payload); err != nil {
			return err
		}
		return models.ProcessInbox(payload.EventID, payload.EventType, func() error {
			(&privateapi.GetOrders{}).RouterInfo().FailureCache(nil)
			(&privateapi.GetOrders{}).RouterInfo().NoticeWebSocket(payload)
			return nil
		})
	})
	if err == nil {
		s.cancels = append(s.cancels, orderCancel)
	}
	for _, subject := range []string{contract.SubjectProductChanged, contract.SubjectOrderChanged} {
		if cancel, subscribeErr := sc.ServiceEventBridge.SubscribeExternalControl(context.Background(), subject); subscribeErr == nil {
			s.cancels = append(s.cancels, cancel)
		}
	}
}
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.cancels {
		cancel()
	}
	s.cancels = nil
}
