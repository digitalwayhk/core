package main

import (
	"github.com/digitalwayhk/core/examples/demo"
	"github.com/digitalwayhk/core/pkg/server/run"
)

func main() {
	//创建WebServer实例
	server := run.NewWebServer()
	//添加OrderService业务服务
	server.AddIService(&demo.DemoService{}, &run.ServerOption{
		//开启跨域，如果不开启，不同ip或端口上运行的前端无法访问api
		IsCors: true,
		//开启websocket,websocke订阅消息格
		// {"channel":"/api/demo/getorder","event":"sub"}
		// {"channel":"/api/demo/getorder","event":"unsub"}
		IsWebSocket: true,
	})
	//启动服务
	server.Start()
}
