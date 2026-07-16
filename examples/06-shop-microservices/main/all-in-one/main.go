package main

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/bootstrap"
	orderservice "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service"
	supplierservice "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service"
	userservice "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// main 仅用于本地断点调试；生产应使用三进程入口。
func main() {
	server := run.NewWebServer()
	server.AddServiceContext(router.NewServiceContextWithConfig(&userservice.Service{}, bootstrap.ServiceConfig("shop-user", 18081, 2, 1)))
	server.SetOption(&userservice.Service{}, &servertypes.ServerOption{IsWebSocket: true})
	server.AddServiceContext(router.NewServiceContextWithConfig(&supplierservice.Service{}, bootstrap.ServiceConfig("shop-supplier", 18082, 3, 2)))
	server.SetOption(&supplierservice.Service{}, &servertypes.ServerOption{IsWebSocket: true})
	server.AddServiceContext(router.NewServiceContextWithConfig(&orderservice.Service{}, bootstrap.ServiceConfig("shop-order", 18083, 4, 3)))
	server.Start()
}
