// 本文件提供 06 微服务示例 all-in-one 调试进程的启动能力。
package main

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/bootstrap"
	orderservice "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service"
	ordermodels "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	supplierservice "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service"
	suppliermodels "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	userservice "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service"
	usermodels "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// main 仅用于本地断点调试；生产应使用三进程入口。
func main() {
	mustInitializeStorage(usermodels.EnsureStorage, suppliermodels.EnsureStorage, ordermodels.EnsureStorage)
	server := run.NewWebServer()
	server.AddServiceContext(router.NewServiceContextWithConfig(&userservice.Service{}, bootstrap.LocalServiceConfig("shop-user", 18081, 2, 1)))
	server.SetOption(&userservice.Service{}, &servertypes.ServerOption{IsWebSocket: true})
	server.AddServiceContext(router.NewServiceContextWithConfig(&supplierservice.Service{}, bootstrap.LocalServiceConfig("shop-supplier", 18082, 3, 2)))
	server.AddServiceContext(router.NewServiceContextWithConfig(&orderservice.Service{}, bootstrap.LocalServiceConfig("shop-order", 18083, 4, 3)))
	server.Start()
}

func mustInitializeStorage(initializers ...func() error) {
	for _, initialize := range initializers {
		if err := initialize(); err != nil {
			panic(err)
		}
	}
}
