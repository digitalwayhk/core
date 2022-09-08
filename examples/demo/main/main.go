package main

import (
	"github.com/digitalwayhk/core/examples/demo/api/private"
	"github.com/digitalwayhk/core/examples/demo/api/public"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

//OrderService 订单服务
type OrderService struct {
}

func (own *OrderService) ServiceName() string {
	return "orders"
}
func (own *OrderService) Routers() []types.IRouter {
	//添加已实现路由添加到OrderService的路由表，用于在Service中自动注册
	routers := []types.IRouter{
		&public.GetOrder{},
		&private.AddOrder{},
	}
	return routers
}
func (own *OrderService) SubscribeRouters() []*types.ObserveArgs {
	return []*types.ObserveArgs{}
}

func main() {
	//创建WebServer实例
	server := run.NewWebServer()
	//添加OrderService服务
	server.AddIService(&OrderService{})
	//启动服务
	server.Start()
}
