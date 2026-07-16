package main

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/bootstrap"
	userservice "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

func main() {
	if err := models.EnsureStorage(); err != nil {
		panic(err)
	}
	server := run.NewWebServer()
	server.AddServiceContext(router.NewServiceContextWithConfig(&userservice.Service{}, bootstrap.ServiceConfig("shop-user", 18081, 2, 1)))
	server.SetOption(&userservice.Service{}, &servertypes.ServerOption{IsWebSocket: true})
	server.Start()
}
