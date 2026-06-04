package main

import (
	"github.com/digitalwayhk/core/examples/06-websocket-local"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

func main() {
	server := run.NewWebServer()
	server.AddIService(&wslocal.MarketService{}, &types.ServerOption{
		IsCors:      true,
		IsWebSocket: true,
	})
	server.Start()
}
