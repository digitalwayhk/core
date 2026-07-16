package main

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/bootstrap"
	supplierservice "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

func main() {
	if err := models.EnsureStorage(); err != nil {
		panic(err)
	}
	server := run.NewWebServer()
	server.AddServiceContext(router.NewServiceContextWithConfig(&supplierservice.Service{}, bootstrap.ServiceConfig("shop-supplier", 18082, 2, 2)))
	server.SetOption(&supplierservice.Service{}, &servertypes.ServerOption{IsWebSocket: true})
	server.Start()
}
