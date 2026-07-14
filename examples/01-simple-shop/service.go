package simpleshop

import (
	"github.com/digitalwayhk/core/examples/01-simple-shop/api/manage"
	privateapi "github.com/digitalwayhk/core/examples/01-simple-shop/api/private"
	publicapi "github.com/digitalwayhk/core/examples/01-simple-shop/api/public"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// ShopService 组装商城的管理、公开和用户私有路由。
type ShopService struct {
	action persistencetypes.IDataAction
}

// NewShopService 创建商城服务，并把同一 IDataAction 注入所有业务路由和校验 hook。
func NewShopService(action persistencetypes.IDataAction) *ShopService {
	if action == nil {
		panic("ShopService 需要显式配置 IDataAction")
	}
	return &ShopService{action: action}
}

// ServiceName 返回配置和路由共同使用的稳定服务名。
func (own *ShopService) ServiceName() string {
	return "shop"
}

// Routers 返回商城全部路由，写权限由各 RouterInfo 类型决定。
func (own *ShopService) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0, 11)
	routers = append(routers, manage.NewProductManage(own.action).Routers()...)
	routers = append(routers, manage.NewOrderManage().Routers()...)
	routers = append(routers,
		publicapi.NewGetProducts(own.action),
		privateapi.NewAddOrder(own.action),
		privateapi.NewGetOrders(own.action),
		privateapi.NewDeleteOrder(own.action),
	)
	return routers
}

// SubscribeRouters 返回内部服务观察订阅；本示例没有跨服务观察者。
func (own *ShopService) SubscribeRouters() []*types.ObserveArgs {
	return nil
}
