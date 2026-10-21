// Package private 演示普通用户通过非owner Gateway查询及订阅本人状态。
package private

import (
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/api/dto"
	publicapi "github.com/digitalwayhk/core/examples/11-scoped-event-ownership/api/public"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/business"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"time"
)

// View 的HTTP/WS身份来自认证上下文，客户端不能伪造归属键或owner。
type View struct {
	userID  string
	runtime *business.Runtime
}

// NewView 注入实例依赖。
func NewView(r *business.Runtime) *View { return &View{runtime: r} }

// New 为请求和会话建立独立路由。
func (v *View) New(interface{}) types.IRouter { return NewView(v.runtime) }

// Parse 不接受任何客户端身份字段。
func (*View) Parse(types.IRequest) error { return nil }

// Validation 校验HTTP身份或框架注入的WS身份。
func (v *View) Validation(req types.IRequest) error {
	if uid, _ := req.GetUser(); uid != "" {
		v.userID = uid
	}
	if v.userID != "A" && v.userID != "B" {
		return types.NewPublicError(types.ErrorKindValidation, 0, "请用演示用户A或B登录", nil)
	}
	return nil
}

// Do 经过请求权限与Ready租约精确调用owner，禁止健康哈希或旧缓存兜底。
func (v *View) Do(req types.IRequest) (interface{}, error) {
	caller, ok := req.(types.IRequestOwnedServiceCaller)
	if !ok {
		return nil, ownership.ErrUnsupported
	}
	api := publicapi.NewSnapshot(nil)
	api.Key = v.userID
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := caller.CallServiceOwned(ctx, api, contract.Scope, v.userID)
	if err != nil {
		return nil, err
	}
	if !res.GetSuccess() {
		return nil, res.GetError()
	}
	return res.GetData(&dto.Snapshot{}), nil
}

// GetResponse 返回独立DTO。
func (*View) GetResponse() interface{} { return &dto.Snapshot{} }

// RouterInfo 保留标准Private认证域。
func (v *View) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(v, router.WithServiceName(contract.ServiceName), router.WithPath("/api/"+contract.ServiceName+"/view"))
}

// SetUserID 接受Core已验证的WS身份。
func (v *View) SetUserID(id, _ string) { v.userID = id }

// GetUserID 返回会话的可信身份。
func (v *View) GetUserID() string { return v.userID }

// GetHashKey 保持原Core WS用户分组机制。
func (v *View) GetHashKey() uint64 { return utils.HashCode64(v.userID) }

// WebSocketOwnedReference 让Core从现有会话生成可恢复的语义引用。
func (v *View) WebSocketOwnedReference() (string, string, string) {
	return contract.Scope, v.userID, "summary"
}

// NoticeFiltersRouter 保留本人投递并校验当前租约，兼容跨节点JSON通知。
func (v *View) NoticeFiltersRouter(message interface{}, api types.IRouter) (bool, interface{}) {
	sub, ok := api.(*View)
	if !ok {
		return false, nil
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return false, nil
	}
	var s dto.Snapshot
	if json.Unmarshal(raw, &s) != nil || s.Key != sub.userID {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !v.runtime.IsCurrentNotice(ctx, &s) {
		return false, nil
	}
	return true, &s
}
