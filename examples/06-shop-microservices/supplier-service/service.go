// 本文件组装当前服务的路由、事件订阅、Outbox 和生命周期能力。
package supplierservice

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	manageapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/manage"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// Service 组装供应商、商品及其跨服务查询路由。
type Service struct {
	mu      sync.Mutex
	cancels []func()
}

// ServiceName 返回供应商服务的稳定逻辑服务名，供路由、发现和内部调用鉴权使用。
func (*Service) ServiceName() string { return contract.SupplierServiceName }

// Routers 注册供应商权威 Public API、供应商/商品 Manage API 和供应商订单投影查询。
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

// SubscribeRouters 保留旧观察路由兼容入口；供应商服务内部事件统一走 EventBridge。
func (*Service) SubscribeRouters() []*servertypes.ObserveArgs { return nil }

// OnAuth 在 TestToken 签发前幂等建立供应商资料；平台管理员不会被误建为供应商。
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

// Start 启用供应商服务 Outbox 发布，并订阅订单事件维护本地 SupplierOrder 投影。
func (s *Service) Start() {
	sc := router.GetContext(contract.SupplierServiceName)
	if sc == nil || sc.ServiceEventBridge == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := sc.UseOutbox(models.OutboxStore{}); err != nil {
		panic(err)
	}
	for _, subscription := range []event.Subscription{
		{Subject: contract.SubjectOrderCreated, EventType: contract.EventOrderCreated, Reliable: true, Handler: handleSupplierOrderEvent},
		{Subject: contract.SubjectOrderStatusChanged, EventType: contract.EventOrderStatusChanged, Reliable: true, Handler: handleSupplierOrderEvent},
		{Subject: contract.SubjectPaymentChanged, EventType: contract.EventPaymentChanged, Reliable: true, Handler: handleSupplierOrderEvent},
	} {
		cancel, err := sc.SubscribeEvent(subscription)
		if err != nil {
			for _, cancel := range s.cancels {
				cancel()
			}
			s.cancels = nil
			panic(err)
		}
		s.cancels = append(s.cancels, cancel)
	}
}

// handleSupplierOrderEvent 消费订单服务发布的订单变化事件，幂等写入供应商本地订单投影。
func handleSupplierOrderEvent(_ context.Context, env *event.Envelope) error {
	payload := &eventdto.OrderChanged{}
	if err := json.Unmarshal(env.Data, payload); err != nil {
		return err
	}
	return models.ApplyOrderEvent(*payload)
}

// Stop 注销供应商服务启动时注册的内部事件订阅。
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.cancels {
		cancel()
	}
	s.cancels = nil
}
