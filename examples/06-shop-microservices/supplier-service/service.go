package supplierservice

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	exampleruntime "github.com/digitalwayhk/core/examples/06-shop-microservices/runtime"
	manageapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/manage"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/zeromicro/go-zero/core/logx"
)

// Service 组装供应商、商品及其跨服务查询路由。
type Service struct {
	mu      sync.Mutex
	worker  *exampleruntime.OutboxWorker
	cancels []func()
}

func (*Service) ServiceName() string { return contract.SupplierServiceName }
func (*Service) Routers() []servertypes.IRouter {
	routers := []servertypes.IRouter{
		&publicapi.GetSuppliers{}, &publicapi.GetProducts{},
	}
	routers = append(routers, manageapi.NewSupplierManage().Routers()...)
	routers = append(routers, manageapi.NewProductManage().Routers()...)
	routers = append(routers, manageapi.NewOrderManage().Routers()...)
	return routers
}

// OnAuthRequest 允许供应商和平台管理员进入统一 Manage，数据范围由 Hook 控制。
func (*Service) OnAuthRequest(ctx context.Context, args servertypes.AuthRequestArgs) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	uid := strings.TrimSpace(args.Identity.UID)
	switch args.PathType {
	case servertypes.ManageType:
		if uid == "" {
			return servertypes.NewPublicError(servertypes.ErrorKindForbidden, servertypes.PublicCodeForbidden, "权限不足", contract.ErrForbidden)
		}
	}
	return nil
}
func (*Service) SubscribeRouters() []*servertypes.ObserveArgs { return nil }

// OnAuth 在 TestToken 签发前幂等建立供应商资料。
// 平台管理员是服务端固定身份，不会被误建为供应商。
func (*Service) OnAuth(_ context.Context, args *servertypes.AuthHookArgs) error {
	if args == nil || strings.TrimSpace(args.UID) == "" {
		return contract.ErrInvalidIdentity
	}
	if args.UID == contract.PlatformAdminUserID {
		return nil
	}
	_, err := business.EnsureSupplier(args.UID, args.Username)
	return err
}

func (s *Service) Start() {
	sc := router.GetContext(contract.SupplierServiceName)
	if sc == nil || sc.ServiceEventBridge == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.worker = exampleruntime.StartOutboxWorker(context.Background(), contract.SupplierServiceName, sc.ServiceEventBridge, func() ([]exampleruntime.OutboxRecord, error) {
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
	orderHandler := func(env *event.Envelope) error {
		payload := &eventdto.OrderChanged{}
		if err := json.Unmarshal(env.Data, payload); err != nil {
			return err
		}
		return models.ApplyOrderEvent(*payload)
	}
	for _, eventType := range []string{contract.EventOrderCreated, contract.EventOrderStatusChanged, contract.EventPaymentChanged} {
		if cancel, subscribeErr := sc.ServiceEventBridge.SubscribeControl(eventType, orderHandler); subscribeErr == nil {
			s.cancels = append(s.cancels, cancel)
		}
	}
	externalCancels, err := exampleruntime.SubscribeExternalControls(context.Background(), sc.ServiceEventBridge,
		contract.SubjectOrderCreated, contract.SubjectOrderStatusChanged, contract.SubjectPaymentChanged)
	if err != nil {
		logx.Errorw("service_external_control_subscribe_failed", logx.Field("service", contract.SupplierServiceName), logx.Field("error", err))
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
	if s.worker != nil {
		s.worker.Stop()
		s.worker = nil
	}
}
