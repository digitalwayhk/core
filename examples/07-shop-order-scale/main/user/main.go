// 本文件提供 07 用户服务独立进程启动能力。
package main

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/bootstrap"
	userservice "github.com/digitalwayhk/core/examples/07-shop-order-scale/user-service"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
)

func main() {
	service := &userservice.Service{}
	server := run.NewWebServer()
	server.AddServiceContext(router.NewServiceContextWithConfig(service, bootstrap.DistributedServiceConfig("shop-user", 18181, 2, 1)))
	server.SetOption(service, bootstrap.SwaggerServerOption(true))
	server.Start()
}
