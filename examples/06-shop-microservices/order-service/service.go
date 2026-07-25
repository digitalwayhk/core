// 本文件组装当前服务的路由、事件订阅、Outbox 和生命周期能力。
package orderservice

import (
	"context"
	"fmt"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	manageapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/manage"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// Service 是订单、支付和事件 Outbox 的事实服务。
type Service struct{}

// ServiceName 返回订单服务的稳定逻辑服务名，供路由、发现和内部调用鉴权使用。
func (*Service) ServiceName() string { return contract.OrderServiceName }

// Routers 注册订单内部 Public API 和平台管理员使用的订单/支付 Manage API。
func (*Service) Routers() []servertypes.IRouter {
	routers := []servertypes.IRouter{&publicapi.CreateOrder{}, &publicapi.CancelOrder{}, &publicapi.CreatePayment{}, &publicapi.GetOrders{}, &publicapi.GetPaymentTypes{}}
	routers = append(routers, manageapi.NewPaymentTypeManage().Routers()...)
	routers = append(routers, manageapi.NewOrderManage().Routers()...)
	routers = append(routers, manageapi.NewPaymentRecordManage().Routers()...)
	return routers
}

// OnAuthRequest 将 Order Manage 限制为平台管理员；内部 Public 由可信调用方白名单校验。
func (*Service) OnAuthRequest(ctx context.Context, args servertypes.AuthRequestArgs) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if args.PathType == servertypes.ManageType && args.Identity.UID != contract.PlatformAdminUserID {
		return servertypes.NewPublicError(servertypes.ErrorKindForbidden, servertypes.PublicCodeForbidden, "权限不足", contract.ErrForbidden)
	}
	return nil
}

// Start 启用订单服务 Outbox，让订单和支付事实变更可靠发布到 EventBridge。
func (s *Service) Start() {
	sc := router.GetContext(contract.OrderServiceName)
	if sc == nil {
		panic(fmt.Errorf("订单服务缺失 ServiceContext: %s", contract.OrderServiceName))
	}
	if err := sc.UseOutbox(models.OutboxStore{}); err != nil {
		panic(err)
	}
}
