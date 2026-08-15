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
)

// main 启动 user、supplier 和 order 服务；WebSocket 只在 user 开启。
func main() {
	mustInitializeStorage(suppliermodels.EnsureStorage, ordermodels.EnsureStorage)
	server := run.NewWebServer()
	// 框架会自动注册一个内置 "server" 系统管理服务，其端口默认 8080/18080，
	// 不受下方各业务服务 LocalServiceConfig 端口参数影响。这里显式指定基准端口，
	// 让内置服务和三个业务服务都基于该基准 + DataCenterID 顺延，避免和默认 8080/18080 冲突。
	server.Port = 48180
	server.GRPCPort = 58180
	// all-in-one 同时启动多个业务 Manage 服务，必须显式选择统一的 Manage Auth
	// 权威服务。框架会在校验和创建 Server 前，将权威服务的完整 ManageAuth
	// 继承给其他业务服务，因此不需要示例代码手工同步随机生成的密钥。
	usersService := &userservice.Service{}
	orderSvc := &orderservice.Service{}
	supplierSvc := &supplierservice.Service{}
	orderConfig := bootstrap.LocalServiceConfig("shop-order", 48183, 4, 3)
	supplierConfig := bootstrap.LocalServiceConfig("shop-supplier", 48182, 3, 2)
	usersConfig := bootstrap.LocalServiceConfig("shop-user", 48181, 2, 1)
	server.ManageAuthAuthorityService = supplierSvc.ServiceName()
	server.AddServiceContext(router.NewServiceContextWithConfig(usersService, usersConfig))
	server.SetOption(usersService, bootstrap.SwaggerServerOption(true))
	server.AddServiceContext(router.NewServiceContextWithConfig(supplierSvc, supplierConfig))
	server.SetOption(supplierSvc, bootstrap.SwaggerServerOption(false))
	server.AddServiceContext(router.NewServiceContextWithConfig(orderSvc, orderConfig))
	server.SetOption(orderSvc, bootstrap.SwaggerServerOption(false))
	server.Start()
}

func mustInitializeStorage(initializers ...func() error) {
	for _, initialize := range initializers {
		if err := initialize(); err != nil {
			panic(err)
		}
	}
}
