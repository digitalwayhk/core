// Package orderservice 组装 07 订单水平扩展服务的路由、事件和生命周期能力。
package orderservice

import (
	"context"
	"fmt"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	manageapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage"
	publicapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/public"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// Service 是可水平扩展的订单权威服务。
type Service struct{}

// ServiceName 返回订单服务稳定逻辑名，多个副本共享该名称。
func (*Service) ServiceName() string { return contract.OrderServiceName }

// Routers 注册订单内部 Public API 和管理员 Manage API。
func (*Service) Routers() []servertypes.IRouter {
	routers := []servertypes.IRouter{&publicapi.CreateOrder{}, &publicapi.CancelOrder{}, &publicapi.CreatePayment{}, &publicapi.GetOrders{}, &publicapi.GetPaymentTypes{}}
	routers = append(routers, manageapi.NewOrderRuleManage().Routers()...)
	routers = append(routers, manageapi.NewPaymentTypeManage().Routers()...)
	routers = append(routers, manageapi.NewOrderManage().Routers()...)
	return routers
}

// OnAuthRequest 将 Order Manage 限制为平台管理员；内部 Public 由 WithInternalCallers 校验。
func (*Service) OnAuthRequest(ctx context.Context, args servertypes.AuthRequestArgs) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if args.PathType == servertypes.ManageType && args.Identity.UID != contract.PlatformAdminUserID {
		return servertypes.NewPublicError(servertypes.ErrorKindForbidden, servertypes.PublicCodeForbidden, "权限不足", contract.ErrForbidden)
	}
	return nil
}

// SubscribeRouters 保留旧观察路由兼容入口；事件订阅统一使用 ServiceEventBridge。
func (*Service) SubscribeRouters() []*servertypes.ObserveArgs { return nil }

// Start 启用订单服务标准 Outbox 发布能力。
func (*Service) Start() {
	sc := router.GetContext(contract.OrderServiceName)
	if sc == nil {
		panic(fmt.Errorf("订单服务缺失 ServiceContext: %s", contract.OrderServiceName))
	}
	if err := sc.UseOutbox(models.OutboxStore{}); err != nil {
		panic(err)
	}
}
