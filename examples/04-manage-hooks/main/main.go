package main

import (
	"github.com/digitalwayhk/core/examples/04-manage-hooks"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

func main() {
	server := run.NewWebServer()
	server.AddIService(&hooks.HookService{}, &types.ServerOption{IsCors: true})
	server.Start()
}
