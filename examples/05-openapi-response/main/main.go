package main

import (
	"github.com/digitalwayhk/core/examples/05-openapi-response"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

func main() {
	server := run.NewWebServer()
	server.AddIService(&openapi.InvoiceService{}, &types.ServerOption{IsCors: true})
	server.Start()
}
