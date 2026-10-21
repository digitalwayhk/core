// 本文件在同一部署保留可选归属WS引用；完整Ready owner与SQL恢复另由示例11回归。
package main

import (
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type ownedView struct{ privateView }

// New 创建独立会话路由，不共享用户身份。
func (*ownedView) New(interface{}) types.IRouter { return &ownedView{} }

// WebSocketOwnedReference 声明归属引用，普通路由不实现此可选接口。
func (v *ownedView) WebSocketOwnedReference() (string, string, string) {
	return contract.Scope, v.userID, "relay-test"
}

// RouterInfo 通过正式RouterInfo注册稳定路径和认证域。
func (v *ownedView) RouterInfo() *types.RouterInfo {
	return router.NewRouterInfoWithOptions(v, "fixture/api/private", "ownedView", router.WithServiceName(contract.ServiceName), router.WithPath("/api/owned-demo/owned"))
}
