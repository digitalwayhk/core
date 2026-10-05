// 本文件提供 09 Manage RBAC 示例的可运行 WebServer 入口。
package main

import (
	adminui "github.com/digitalwayhk/core/examples/08-admin-manage-ui"
	"github.com/digitalwayhk/core/examples/08-admin-manage-ui/models"
	"github.com/digitalwayhk/core/pkg/server/run"
)

func main() {
	if err := models.EnsureStorage(); err != nil {
		panic(err)
	}
	server := run.NewWebServer()
	server.AddIService(&adminui.CatalogService{})
	server.Start()
}
