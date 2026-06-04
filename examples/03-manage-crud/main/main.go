package main

import (
	"github.com/digitalwayhk/core/examples/03-manage-crud"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

func main() {
	// 管理端示例服务。前端 WayPlus 可以读取 View schema 自动生成页面。
	server := run.NewWebServer()
	server.AddIService(&managecrud.CatalogService{}, &types.ServerOption{
		IsCors: true,
	})
	server.Start()
}
