// 本文件提供 07 订单水平扩展示例 all-in-one 调试进程的启动能力。
package main

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/bootstrap"
	orderservice "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service"
	ordermodels "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	supplierservice "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service"
	suppliermodels "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models"
	userservice "github.com/digitalwayhk/core/examples/07-shop-order-scale/user-service"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// main 启动 user、supplier 和 order 服务；WebSocket 只在 user 开启。
func main() {
	mustInitializeStorage(suppliermodels.EnsureStorage, ordermodels.EnsureStorage)
	server := run.NewWebServer()
	server.AddServiceContext(router.NewServiceContextWithConfig(&userservice.Service{}, bootstrap.LocalServiceConfig("shop-user", 18181, 2, 1)))
	server.SetOption(&userservice.Service{}, &servertypes.ServerOption{IsWebSocket: true})
	server.AddServiceContext(router.NewServiceContextWithConfig(&supplierservice.Service{}, bootstrap.LocalServiceConfig("shop-supplier", 18182, 3, 2)))
	server.AddServiceContext(router.NewServiceContextWithConfig(&orderservice.Service{}, bootstrap.LocalServiceConfig("shop-order", 18183, 4, 3)))
	server.Start()
}

func mustInitializeStorage(initializers ...func() error) {
	for _, initialize := range initializers {
		if err := initialize(); err != nil {
			panic(err)
		}
	}
}
