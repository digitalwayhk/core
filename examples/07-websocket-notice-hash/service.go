package wshash

import (
	"github.com/digitalwayhk/core/examples/07-websocket-notice-hash/api/public"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// AccountService 演示 hash 定向推送。
type AccountService struct{}

func (own *AccountService) ServiceName() string { return "account" }

func (own *AccountService) Routers() []types.IRouter {
	return []types.IRouter{
		&public.WatchBalance{},
		&public.PushBalance{},
	}
}

func (own *AccountService) SubscribeRouters() []*types.ObserveArgs { return nil }
