// 本文件保留现有路由调用权限和可信调用方身份，仅将选址换成Ready租约。
package router

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// CallServiceOwned 保留callPayload权限校验，业务不自行访问发现或Redis锁。
func (r *Request) CallServiceOwned(ctx context.Context, api types.IRouter, scope, key string) (types.IResponse, error) {
	payload, err := r.callPayload(api)
	if err != nil {
		return nil, err
	}
	return r.service.CallServiceOwned(ctx, payload, scope, key)
}

// CallServiceOwned 保留可信内部请求包装前的可选归属调用能力。
func (r *trustedInternalRequest) CallServiceOwned(ctx context.Context, api types.IRouter, scope, key string) (types.IResponse, error) {
	caller, ok := r.IRequest.(types.IRequestOwnedServiceCaller)
	if !ok {
		return nil, ownership.ErrUnsupported
	}
	return caller.CallServiceOwned(ctx, api, scope, key)
}
