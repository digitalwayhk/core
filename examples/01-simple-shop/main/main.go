package main

import (
	"strings"

	simpleshop "github.com/digitalwayhk/core/examples/01-simple-shop"
	"github.com/digitalwayhk/core/examples/01-simple-shop/contract"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// main 启动框架内建管理服务和最简商城服务。
func main() {
	const authority = contract.ServiceName
	server := run.NewWebServer()
	server.AddIService(&simpleshop.ShopService{}, &types.ServerOption{IsWebSocket: true})
	syncManageAuthFromAuthority(authority, "server")
	server.Start()
}

// syncManageAuthFromAuthority 对齐权威服务与 peer 的 ManageAuth 字段，
// 避免多 Manage 服务时出现认证门禁不一致。
func syncManageAuthFromAuthority(authorityService string, peers ...string) {
	authority := strings.ToLower(strings.TrimSpace(authorityService))
	authCtx := router.GetContext(authority)
	if authCtx == nil || authCtx.Config == nil {
		return
	}
	src := authCtx.Config.ManageAuth
	if len(peers) == 0 {
		peers = []string{"server"}
	}
	for _, peer := range peers {
		name := strings.ToLower(strings.TrimSpace(peer))
		if name == "" || name == authority {
			continue
		}
		ctx := router.GetContext(name)
		if ctx == nil || ctx.Config == nil {
			continue
		}
		ctx.Config.ManageAuth.AccessSecret = src.AccessSecret
		ctx.Config.ManageAuth.RefreshSecret = src.RefreshSecret
		ctx.Config.ManageAuth.AccessExpire = src.AccessExpire
		ctx.Config.ManageAuth.RefreshExpire = src.RefreshExpire
		ctx.Config.ManageAuth.CasDoor.Enable = src.CasDoor.Enable
	}
}
