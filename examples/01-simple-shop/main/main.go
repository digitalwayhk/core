package main

import (
	simpleshop "github.com/digitalwayhk/core/examples/01-simple-shop"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// main 启动框架内建管理服务和最简商城服务。
func main() {
	server := run.NewWebServer()
	server.AddIService(&simpleshop.ShopService{}, &types.ServerOption{IsWebSocket: true})
	server.Start()
}
