package orderservice

import (
	"context"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	manageapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/manage"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// Service 是订单、支付和事件 Outbox 的事实服务。
type Service struct{}

func (*Service) ServiceName() string { return contract.OrderServiceName }
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
func (*Service) SubscribeRouters() []*servertypes.ObserveArgs { return nil }
func (s *Service) Start() {
	sc := router.GetContext(contract.OrderServiceName)
	if sc == nil || sc.ServiceEventBridge == nil {
		return
	}
	if err := sc.UseOutbox(models.OutboxStore{}); err != nil {
		panic(err)
	}
}
