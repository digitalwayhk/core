// Package userservice 组装 07 普通用户入口服务路由能力。
package userservice

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	privateapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/user-service/api/private"
	publicapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/user-service/api/public"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// Service 是普通用户入口服务。
type Service struct{}

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
