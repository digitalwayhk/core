package wslocal

import (
	"github.com/digitalwayhk/core/examples/06-websocket-local/api/public"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// MarketService 演示本机 WebSocket 推送。
type MarketService struct{}

func (own *MarketService) ServiceName() string { return "market" }

func (own *MarketService) Routers() []types.IRouter {
	return []types.IRouter{
		&public.WatchPrice{},
		&public.PublishPrice{},
	}
}

func (own *MarketService) SubscribeRouters() []*types.ObserveArgs { return nil }
