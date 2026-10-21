// Package public 只暴露经可信内部调用校验的owner快照。
package public

import (
	"context"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/api/dto"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/business"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// Snapshot 是只供同逻辑服务内部Gateway调用的查询。
type Snapshot struct {
	Key     string `json:"key"`
	runtime *business.Runtime
}

// NewSnapshot 注入本实例依赖，跨进程接收端使用自己的实例依赖。
func NewSnapshot(r *business.Runtime) *Snapshot { return &Snapshot{runtime: r} }

// New 保留实例依赖，不复用请求字段。
func (s *Snapshot) New(interface{}) types.IRouter { return NewSnapshot(s.runtime) }

// Parse 绑定可信内部参数。
func (s *Snapshot) Parse(req types.IRequest) error { return req.Bind(s) }

// Validation 只允许显式演示键。
func (s *Snapshot) Validation(types.IRequest) error {
	if s.Key != "A" && s.Key != "B" {
		return types.NewPublicError(types.ErrorKindValidation, 0, "仅支持演示用户A/B", nil)
	}
	return nil
}

// Do 在目标副本再次验证本地有效owner。
func (s *Snapshot) Do(types.IRequest) (interface{}, error) {
	return s.runtime.Read(context.Background(), s.Key)
}

// GetResponse 返回独立DTO。
func (*Snapshot) GetResponse() interface{} { return &dto.Snapshot{} }

// RouterInfo 显式稳定路径和内部调用白名单；匿名HTTP不能建立可信来源。
func (s *Snapshot) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(s, router.WithServiceName(contract.ServiceName), router.WithPath("/api/"+contract.ServiceName+"/snapshot"), router.WithPathType(types.PublicType), router.WithAuth(false), router.WithInternalCallers(contract.ServiceName))
}
