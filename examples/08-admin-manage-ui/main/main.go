package main

import (
	adminui "github.com/digitalwayhk/core/examples/08-admin-manage-ui"
	"github.com/digitalwayhk/core/pkg/server/run"
)

// main 启动框架内建管理服务和资料目录服务。
func main() {
	server := run.NewWebServer()
	server.AddIService(&adminui.CatalogService{})
	server.Start()
}
