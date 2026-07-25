// 本文件提供 07 可水平扩展订单服务独立进程启动能力。
package main

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/bootstrap"
	orderservice "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
)

func main() {
	if err := models.EnsureStorage(); err != nil {
		panic(err)
	}
	httpPort := bootstrap.OrderHTTPPort()
	server := run.NewWebServer()
	server.AddServiceContext(router.NewServiceContextWithConfig(&orderservice.Service{}, bootstrap.DistributedOrderConfig(httpPort, 4)))
	server.Start()
}
