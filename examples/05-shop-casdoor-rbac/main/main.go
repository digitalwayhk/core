package main

import (
	casdoorrbacshop "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// main 启动框架内建管理服务和模型、Manage 继承商城示例。
func main() {
	server := run.NewWebServer()
	server.AddIService(&casdoorrbacshop.ShopService{}, &types.ServerOption{IsWebSocket: true})
	server.Start()
}
