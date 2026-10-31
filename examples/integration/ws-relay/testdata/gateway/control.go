// 本文件只给隔离测试观察真实订阅索引并通过正式NoticeWebSocket发布，不替代任何中继。
package main

import (
	"context"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"os"
)

type control struct {
	Route    string `json:"route"`
	UserID   string `json:"userID"`
	Sequence int    `json:"sequence"`
}

// Parse 使用Core绑定测试参数，不读取客户端身份。
func (v *control) Parse(req types.IRequest) error { return req.Bind(v) }

// Validation 校验对应认证域与可信会话身份。
func (*control) Validation(req types.IRequest) error {
	uid, _ := req.GetUser()
	if uid != "A" {
		return types.ErrInternalCallerForbidden
	}
	return nil
}

// Do 执行隔离测试动作或返回独立响应。
func (v *control) Do(types.IRequest) (interface{}, error) {
	sc := router.GetContext(contract.ServiceName)
	paths := []string{"/api/owned-demo/ordinary-private", "/api/owned-demo/ordinary-public", "/api/owned-demo/owned"}
	local, peer := map[string][]uint64{}, map[string][]uint64{}
	for _, path := range paths {
		info := sc.Router.GetRouter(path)
		local[path] = sc.RouteWebSocketHub.SubscribedHashes(info)
		peer[path] = sc.CrossNodeBroker.PeerSubscribedHashes(path)
		if v.Sequence > 0 && v.Route == path {
			info.NoticeWebSocket(map[string]interface{}{"userID": v.UserID, "sequence": v.Sequence})
		}
	}
	nodes, err := sc.ClusterProvider.List(context.Background(), contract.ServiceName)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"pid": os.Getpid(), "local": local, "peer": peer, "nodes": nodes}, nil
}

// RouterInfo 通过正式RouterInfo注册稳定路径和认证域。
func (v *control) RouterInfo() *types.RouterInfo {
	return router.NewRouterInfoWithOptions(v, "fixture/api/private", "control", router.WithServiceName(contract.ServiceName), router.WithPath("/api/owned-demo/control"))
}
