package demo

import (
	"github.com/digitalwayhk/core/examples/demo/api/manage"
	"github.com/digitalwayhk/core/examples/demo/api/private"
	"github.com/digitalwayhk/core/examples/demo/api/public"
	"github.com/digitalwayhk/core/examples/demo/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// DemoService 主服务，用于在WebServer提供服务，包含路由表，订阅路由表
type DemoService struct {
}

// ServiceName 服务的名称
func (own *DemoService) ServiceName() string {
	return "demo"
}

// Routers DemoService的路由表，服务中的所有路由通过此方法注册
func (own *DemoService) Routers() []types.IRouter {
	//添加已实现路由添加到OrderService的路由表，用于在Service中自动注册
	routers := make([]types.IRouter, 0)

	//api/public 文件夹下的公共路由
	//public路由，websocket订阅时，通过/ws连接后，send {"channel":"/api/demo/getorder","event":"sub"} 订阅
	//http post /api/demo/getorder
	routers = append(routers, &public.GetOrder{})
	//http post /api/demo/gettoken
	routers = append(routers, &public.GetToken{})

	//api/private 文件夹下的私有路由,必须获取token才能访问
	//private的路由，websocket订阅时，必须是通过/wsauth连接，才能订阅到
	//http post /api/demo/addorder
	routers = append(routers, &private.AddOrder{})

	//api/manage 文件夹下的管理路由,管理路由包含增删改查路由
	routers = append(routers, manage.NewTokenManage().Routers()...)

	return routers
}

// SubscribeRouters 服务的订阅路由，通过该方法订阅路由获取通知消息
func (own *DemoService) SubscribeRouters() []*types.ObserveArgs {
	//订阅演示，通过websocket订阅/api/demo/getorder路由，当/api/demo/addorder被调用并成功执行后，会通过websocket发送新add的order到客户端
	return []*types.ObserveArgs{
		types.NewObserveArgs( //创建一个订阅参数
			&private.AddOrder{},   //订阅的新增订单路由
			types.ObserveResponse, //订阅的事件类型，ObserveResponse是指当AddOrder路由被调用并成功返回时，会触发该订阅通知下面的回调函数
			func(args *types.NotifyArgs) error {
				//TODO 这里是处理新增订单的消息，推送到通过websocket订阅的GetOrder的客户端
				api := &public.GetOrder{}
				info := api.RouterInfo()
				order := router.GetResponseData[models.OrderModel](args.Response) //从推送过来的消息中获取订单数据
				//获取所有订阅的路由类型，当订阅路由的data中参数值不同时，会有多个订阅路由类型
				//for _, r := range info.GetWebSocketIRouter() {
				//推送刚创建的order到通过websocket订阅GetOrder路由的客户端
				//这里推送到所有订阅（100个用户订阅一次推送），实际使用中，可以根据条件推送到指定的客户端
				//例如：根据order的用户id，推送到指定的客户端 r.(*public.GetOrder).UserID == order.UserID
				info.NoticeWebSocket(order)
				//}
				return nil
			}),
	}
}

// Start 服务启动后，只执行行一次，可以在该方法内做一些初始化操作，也可以不写
// func (own *DemoService) Start() {
// }

// IsCloseServerManage 是否关闭本服务的管理，如果关闭，服务管理将不会在服务启动后自动注册servermanage路由
// func (own *DemoService) IsCloseServerManage() bool {
// 	return true
// }

//Stop 停止服务完成时调用
// func (own *DemoService) Stop() {
// }
