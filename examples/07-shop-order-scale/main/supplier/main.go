// 本文件提供 07 供应商服务独立进程启动能力。
package main

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/bootstrap"
	supplierservice "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
)

func main() {
	if err := models.EnsureStorage(); err != nil {
		panic(err)
	}
	server := run.NewWebServer()
	server.AddServiceContext(router.NewServiceContextWithConfig(&supplierservice.Service{}, bootstrap.DistributedServiceConfig("shop-supplier", 18182, 3, 1)))
	server.Start()
}
