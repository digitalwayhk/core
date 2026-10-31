// 本文件定义匿名可订阅的普通Public WS，不声明归属能力。
package main

import (
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type publicView struct{ privateView }

// New 创建独立会话路由，不共享用户身份。
func (*publicView) New(interface{}) types.IRouter { return &publicView{} }

// Validation 校验对应认证域与可信会话身份。
func (v *publicView) Validation(types.IRequest) error { v.userID = "public"; return nil }

// RouterInfo 通过正式RouterInfo注册稳定路径和认证域。
func (v *publicView) RouterInfo() *types.RouterInfo {
	return router.NewRouterInfoWithOptions(v, "fixture/api/public", "publicView", router.WithServiceName(contract.ServiceName), router.WithPath("/api/owned-demo/ordinary-public"))
}
