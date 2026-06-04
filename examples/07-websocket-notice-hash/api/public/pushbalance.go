package public

import (
	"errors"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// PushBalance 模拟服务端余额变化，并触发 WebSocket 定向推送。
type PushBalance struct {
	UserID  string `json:"userid" desc:"用户 ID"`
	Balance string `json:"balance" desc:"余额"`
}

func (own *PushBalance) Parse(req types.IRequest) error { return req.Bind(own) }

func (own *PushBalance) Validation(req types.IRequest) error {
	if own.UserID == "" {
		return errors.New("userid 不能为空")
	}
	return nil
}

func (own *PushBalance) Do(req types.IRequest) (interface{}, error) {
	msg := &BalanceMessage{UserID: own.UserID, Balance: own.Balance}
	(&WatchBalance{UserID: own.UserID}).RouterInfo().NoticeWebSocket(msg)
	return msg, nil
}

func (own *PushBalance) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfo(own)
}
