// 本路由仅验证既有内部白名单早拒绝与真实mTLS调用，不开放注册或管理权限。
package main

import (
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"sync/atomic"
)

var internalProbeParses atomic.Int32

type internalProbe struct {
	Value string `json:"value"`
}

func (p *internalProbe) Parse(req types.IRequest) error {
	internalProbeParses.Add(1)
	return req.Bind(p)
}
func (*internalProbe) Validation(types.IRequest) error { return nil }
func (*internalProbe) Do(types.IRequest) (interface{}, error) {
	return map[string]bool{"ok": true}, nil
}
func (p *internalProbe) RouterInfo() *types.RouterInfo {
	return router.NewRouterInfoWithOptions(p, "fixture/api/public", "internalProbe", router.WithServiceName(contract.ServiceName), router.WithPath("/api/owned-demo/internal-probe"), router.WithInternalCallers(contract.ServiceName))
}
