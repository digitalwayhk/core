package main

import (
	inheritanceshop "github.com/digitalwayhk/core/examples/03-shop-inheritance"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// main 启动框架内建管理服务和模型、Manage 继承商城示例。
func main() {
	server := run.NewWebServer()
	server.AddIService(&inheritanceshop.ShopService{}, &types.ServerOption{IsWebSocket: true})
	server.Start()
}
