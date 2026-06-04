package main

import (
	"github.com/digitalwayhk/core/examples/01-hello-router"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

func main() {
	// 创建 WebServer。命令行可以通过 -p 指定 HTTP 端口。
	server := run.NewWebServer()

	// 注册最小服务。开启 CORS 方便浏览器或前端页面直接调用。
	server.AddIService(&hello.HelloService{}, &types.ServerOption{
		IsCors: true,
	})

	// 启动服务，默认会监听 8080；本示例 README 建议使用 -p 18081。
	server.Start()
}
