package hello

import (
	"github.com/digitalwayhk/core/examples/01-hello-router/api/public"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// HelloService 是最小业务服务，只注册一个 public 路由。
type HelloService struct{}

// ServiceName 返回服务名。public 路由会自动生成 /api/hello/{routerName}。
func (own *HelloService) ServiceName() string {
	return "hello"
}

// Routers 返回服务对外提供的路由列表。
func (own *HelloService) Routers() []types.IRouter {
	return []types.IRouter{
		&public.Ping{},
	}
}

// SubscribeRouters 本示例没有订阅其他路由，因此返回 nil。
func (own *HelloService) SubscribeRouters() []*types.ObserveArgs {
	return nil
}
