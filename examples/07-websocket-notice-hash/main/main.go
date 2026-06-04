package main

import (
	"github.com/digitalwayhk/core/examples/07-websocket-notice-hash"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

func main() {
	server := run.NewWebServer()
	server.AddIService(&wshash.AccountService{}, &types.ServerOption{
		IsCors:      true,
		IsWebSocket: true,
	})
	server.Start()
}
