package main

import (
	simpleshop "github.com/digitalwayhk/core/examples/01-simple-shop"
	"github.com/digitalwayhk/core/examples/01-simple-shop/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// main 启动框架内建管理服务和最简商城服务。
func main() {
	server := run.NewWebServer()
	action := entity.GetGlobalSqliteInstance(models.NewProduct().GetLocalDBName())
	server.AddIService(simpleshop.NewShopService(action), &types.ServerOption{IsWebSocket: true})
	server.Start()
}
