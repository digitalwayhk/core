package orderservice

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	privateapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/private"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// Service 是订单、支付和事件 Outbox 的事实服务。
type Service struct{}

func (*Service) ServiceName() string { return contract.OrderServiceName }
func (*Service) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{&privateapi.CreateOrder{}, &privateapi.GetUserOrders{}, &privateapi.GetSupplierOrders{}, &privateapi.DeleteOrder{}}
}
func (*Service) SubscribeRouters() []*servertypes.ObserveArgs { return nil }
