package paymentshop

import (
	"github.com/digitalwayhk/core/examples/02-shop-payment/api/manage"
	privateapi "github.com/digitalwayhk/core/examples/02-shop-payment/api/private"
	publicapi "github.com/digitalwayhk/core/examples/02-shop-payment/api/public"
	"github.com/digitalwayhk/core/examples/02-shop-payment/contract"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// ShopService 组装带支付能力的完整商城示例。
type ShopService struct{}

// ServiceName 返回支付商城的稳定服务名。
func (own *ShopService) ServiceName() string { return contract.ServiceName }

// Routers 返回支付商城的 Manage、Public 和 Private 路由。
func (own *ShopService) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0, 26)
	routers = append(routers, manage.NewProductManage().Routers()...)
	routers = append(routers, manage.NewPaymentTypeManage().Routers()...)
	routers = append(routers, manage.NewOrderManage().Routers()...)
	routers = append(routers, manage.NewPaymentRecordManage().Routers()...)
	routers = append(routers,
		&publicapi.GetProducts{},
		&publicapi.GetPaymentTypes{},
		&privateapi.AddOrder{},
		&privateapi.GetOrders{},
		&privateapi.DeleteOrder{},
		&privateapi.CreatePayment{},
		&privateapi.CancelOrder{},
	)
	return routers
}

// SubscribeRouters 返回内部服务观察订阅；本示例没有跨服务订阅。
func (own *ShopService) SubscribeRouters() []*types.ObserveArgs { return nil }
