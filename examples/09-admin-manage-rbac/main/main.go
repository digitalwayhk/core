// 本文件提供 09 Manage RBAC 示例的可运行 WebServer 入口。
package main

import (
	"github.com/digitalwayhk/core/pkg/server/run"
)

func main() {
	server := run.NewWebServer()
	server.Start()
}
