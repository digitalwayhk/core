package simpleshop

import (
	"context"
	"errors"

	"github.com/digitalwayhk/core/examples/01-simple-shop/api/manage"
	privateapi "github.com/digitalwayhk/core/examples/01-simple-shop/api/private"
	publicapi "github.com/digitalwayhk/core/examples/01-simple-shop/api/public"
	"github.com/digitalwayhk/core/examples/01-simple-shop/contract"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// ShopService 组装商城的管理、公开和用户私有路由。
type ShopService struct{}

// OnAuth 演示服务如何在框架签名 Access Token 前注入最小业务 Claim。
// Refresh Token 不会携带该字段，刷新时框架会重新调用本钩子。
func (*ShopService) OnAuth(_ context.Context, args *types.AuthHookArgs) error {
	if args == nil || args.Claims == nil {
		return errors.New("认证钩子参数不完整")
	}
	args.Claims.AddData("example_service", contract.ServiceName)
	return nil
}

// ServiceName 返回配置和路由共同使用的稳定服务名。
func (own *ShopService) ServiceName() string {
	return contract.ServiceName
}

// Routers 返回商城全部路由，写权限由各 RouterInfo 类型决定。
func (own *ShopService) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0, 11)
	routers = append(routers, manage.NewProductManage().Routers()...)
	routers = append(routers, manage.NewOrderManage().Routers()...)
	routers = append(routers,
		&publicapi.GetProducts{},
		&privateapi.AddOrder{},
		&privateapi.GetOrders{},
		&privateapi.DeleteOrder{},
	)
	return routers
}

// SubscribeRouters 返回内部服务观察订阅；本示例没有跨服务观察者。
func (own *ShopService) SubscribeRouters() []*types.ObserveArgs {
	return nil
}
