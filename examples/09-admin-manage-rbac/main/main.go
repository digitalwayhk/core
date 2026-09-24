package main

import (
	adminrbac "github.com/digitalwayhk/core/examples/09-admin-manage-rbac"
	"github.com/digitalwayhk/core/pkg/server/run"
)

func main() {
	server := run.NewWebServer()
	server.AddIService(adminrbac.NewAdminService())
	server.Start()
}
