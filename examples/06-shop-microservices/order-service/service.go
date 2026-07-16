package orderservice

import (
	"context"
	"sync"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	manageapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/manage"
	privateapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/private"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	exampleruntime "github.com/digitalwayhk/core/examples/06-shop-microservices/runtime"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// Service 是订单、支付和事件 Outbox 的事实服务。
type Service struct {
	mu     sync.Mutex
	worker *exampleruntime.OutboxWorker
}

func (*Service) ServiceName() string { return contract.OrderServiceName }
func (*Service) Routers() []servertypes.IRouter {
	routers := []servertypes.IRouter{&publicapi.GetPaymentTypes{}, &privateapi.CreateOrder{}, &privateapi.GetUserOrders{}, &privateapi.GetSupplierOrders{}, &privateapi.DeleteOrder{}, &privateapi.CreatePayment{}, &manageapi.ConfirmPayment{}}
	routers = append(routers, manageapi.NewPaymentTypeManage().Routers()...)
	routers = append(routers, manageapi.NewOrderManage().Routers()...)
	routers = append(routers, manageapi.NewPaymentRecordManage().Routers()...)
	return routers
}
func (*Service) SubscribeRouters() []*servertypes.ObserveArgs { return nil }
func (s *Service) Start() {
	sc := router.GetContext(contract.OrderServiceName)
	if sc == nil || sc.ServiceEventBridge == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.worker = exampleruntime.StartOutboxWorker(context.Background(), contract.OrderServiceName, sc.ServiceEventBridge, func() ([]exampleruntime.OutboxRecord, error) {
		items, err := models.PendingOutbox()
		if err != nil {
			return nil, err
		}
		result := make([]exampleruntime.OutboxRecord, 0, len(items))
		for _, item := range items {
			result = append(result, exampleruntime.OutboxRecord{ID: item.ID, EventID: item.EventID, EventType: item.EventType, Subject: item.Subject, Payload: item.Payload})
		}
		return result, nil
	}, func(record exampleruntime.OutboxRecord) error {
		items, err := models.PendingOutbox()
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.ID == record.ID {
				return models.MarkOutboxPublished(item)
			}
		}
		return nil
	})
}
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.worker != nil {
		s.worker.Stop()
		s.worker = nil
	}
}
