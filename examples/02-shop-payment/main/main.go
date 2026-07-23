package main

import (
	paymentshop "github.com/digitalwayhk/core/examples/02-shop-payment"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// main 启动框架内建管理服务和带支付能力的商城示例。
func main() {
	server := run.NewWebServer()
	server.AddIService(&paymentshop.ShopService{}, &types.ServerOption{IsWebSocket: true})
	server.Start()
}
