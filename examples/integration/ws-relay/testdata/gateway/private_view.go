// 本文件定义没有归属接口的普通Private WS，身份只读认证上下文及Core注入会话。
package main

import (
	"encoding/json"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type privateView struct{ userID string }

// New 创建独立会话路由，不共享用户身份。
func (v *privateView) New(interface{}) types.IRouter { return &privateView{} }

// Parse 使用Core绑定测试参数，不读取客户端身份。
func (*privateView) Parse(types.IRequest) error { return nil }

// Validation 校验对应认证域与可信会话身份。
func (v *privateView) Validation(req types.IRequest) error {
	if uid, _ := req.GetUser(); uid != "" {
		v.userID = uid
	}
	if v.userID == "" {
		return types.ErrInternalCallerForbidden
	}
	return nil
}

// Do 执行隔离测试动作或返回独立响应。
func (*privateView) Do(types.IRequest) (interface{}, error) { return map[string]bool{"ok": true}, nil }

// RouterInfo 通过正式RouterInfo注册稳定路径和认证域。
func (v *privateView) RouterInfo() *types.RouterInfo {
	return router.NewRouterInfoWithOptions(v, "fixture/api/private", "privateView", router.WithServiceName(contract.ServiceName), router.WithPath("/api/owned-demo/ordinary-private"))
}

// SetUserID 仅接收Core已经验证的会话身份。
func (v *privateView) SetUserID(id, _ string) { v.userID = id }

// GetUserID 返回Core注入的会话用户。
func (v *privateView) GetUserID() string { return v.userID }

// GetHashKey 按可信用户生成稳定订阅分组。
func (v *privateView) GetHashKey() uint64 { return utils.HashCode64(v.userID) }

// NoticeFiltersRouter 只投递给对应用户，兼容跨节点JSON通知。
func (*privateView) NoticeFiltersRouter(message interface{}, api types.IRouter) (bool, interface{}) {
	raw, err := json.Marshal(message)
	if err != nil {
		return false, nil
	}
	var value struct {
		UserID   string `json:"userID"`
		Sequence int    `json:"sequence"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return false, nil
	}
	identity, ok := api.(interface{ GetUserID() string })
	return ok && identity.GetUserID() == value.UserID, value
}
