package public

import (
	"errors"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// PublishPrice 演示通过 HTTP 触发 WebSocket 推送。
type PublishPrice struct {
	Symbol string `json:"symbol" desc:"交易对"`
	Price  string `json:"price" desc:"最新价格"`
}

func (own *PublishPrice) Parse(req types.IRequest) error { return req.Bind(own) }

func (own *PublishPrice) Validation(req types.IRequest) error {
	if own.Symbol == "" {
		return errors.New("symbol 不能为空")
	}
	if own.Price == "" {
		return errors.New("price 不能为空")
	}
	return nil
}

func (own *PublishPrice) Do(req types.IRequest) (interface{}, error) {
	message := map[string]string{"symbol": own.Symbol, "price": own.Price}
	(&WatchPrice{}).RouterInfo().NoticeWebSocket(message)
	return message, nil
}

func (own *PublishPrice) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfo(own)
}
