package main

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/bootstrap"
	orderservice "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
)

func main() {
	server := run.NewWebServer()
	server.AddServiceContext(router.NewServiceContextWithConfig(&orderservice.Service{}, bootstrap.ServiceConfig("shop-order", 18083, 2, 3)))
	server.Start()
}
