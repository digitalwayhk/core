package main

import (
	"github.com/digitalwayhk/core/examples/demo"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

func main() {
	//defer profile.Start().Stop()
	//defer profile.Start(profile.MemProfile, profile.MemProfileRate(1)).Stop()
	//创建WebServer实例
	server := run.NewWebServer()
	//添加DemoService业务服务
	server.AddIService(&demo.DemoService{}, &types.ServerOption{
		//开启跨域，如果不开启，不同ip或端口上运行的前端无法访问api
		IsCors: true,
		//开启websocket,所有public和private的api都可以被websocke订阅，消息格式如下
		// {"channel":"/api/demo/getorder","event":"sub"} 订阅路由
		// {"channel":"/api/demo/getorder","event":"unsub"} 退订路由
		IsWebSocket: true,
	})
	//启动服务
	server.Start()
}
