package public

import (
	"hash/fnv"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// BalanceMessage 是推送给前端的余额消息。
type BalanceMessage struct {
	UserID  string `json:"userid"`
	Balance string `json:"balance"`
}

// WatchBalance 表示“订阅某个用户的余额变化”。
type WatchBalance struct {
	UserID string `json:"userid" desc:"用户 ID"`
}

func (own *WatchBalance) Parse(req types.IRequest) error { return req.Bind(own) }

func (own *WatchBalance) Validation(req types.IRequest) error { return nil }

func (own *WatchBalance) Do(req types.IRequest) (interface{}, error) {
	return map[string]string{"userid": own.UserID, "state": "watching"}, nil
}

func (own *WatchBalance) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfo(own)
}

// GetHashKey 告诉框架该订阅的 hash。跨节点时可维护 routePath + hash 的本地索引。
func (own *WatchBalance) GetHashKey() uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(own.UserID))
	return h.Sum64()
}

// NoticeFiltersRouter 判断推送消息是否命中当前订阅路由。
func (own *WatchBalance) NoticeFiltersRouter(message interface{}, api types.IRouter) (bool, interface{}) {
	msg, ok := message.(*BalanceMessage)
	if !ok {
		return false, nil
	}
	return msg.UserID == own.UserID, msg
}
