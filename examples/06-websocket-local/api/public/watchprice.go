package public

import (
	"sync"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

var priceSubscribers sync.Map

// WatchPrice 是 WebSocket 订阅路由。客户端订阅后会登记 symbol。
type WatchPrice struct {
	Symbol string `json:"symbol" desc:"交易对"`
}

func (own *WatchPrice) Parse(req types.IRequest) error { return req.Bind(own) }

func (own *WatchPrice) Validation(req types.IRequest) error {
	if own.Symbol == "" {
		own.Symbol = "BTCUSDT"
	}
	return nil
}

func (own *WatchPrice) Do(req types.IRequest) (interface{}, error) {
	return map[string]string{"symbol": own.Symbol, "state": "subscribed"}, nil
}

func (own *WatchPrice) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfo(own)
}

// RegisterWebSocket 在客户端订阅时被框架调用。
func (own *WatchPrice) RegisterWebSocket(client types.IWebSocket, req types.IRequest) {
	priceSubscribers.Store(client, own.Symbol)
}

// UnRegisterWebSocket 在客户端退订或断开时被框架调用。
func (own *WatchPrice) UnRegisterWebSocket(client types.IWebSocket, req types.IRequest) {
	priceSubscribers.Delete(client)
}
