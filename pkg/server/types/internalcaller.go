package types

import (
	"context"
	"errors"
	"strings"
)

// ErrInternalCallerForbidden 表示路由拒绝了缺失、伪造或未列入白名单的内部调用方。
var ErrInternalCallerForbidden = errors.New("可信内部调用方无权访问该路由")

// IRequestInternalCaller 是 IRequest 的可选可信调用方读取契约。
// 不把它并入稳定 IRequest，避免破坏已有 mock 和自定义请求实现。
type IRequestInternalCaller interface {
	TrustedInternalCaller() (string, bool)
}

type trustedInternalCallerContextKey struct{}

// ContextWithTrustedInternalCaller 仅供经过框架信任边界验证的调用链写入服务身份。
func ContextWithTrustedInternalCaller(ctx context.Context, service string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, trustedInternalCallerContextKey{}, strings.TrimSpace(service))
}

// TrustedInternalCallerFromContext 读取框架信任边界写入的服务身份。
func TrustedInternalCallerFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	service, ok := ctx.Value(trustedInternalCallerContextKey{}).(string)
	service = strings.TrimSpace(service)
	return service, ok && service != ""
}

// AuthorizeInternalCaller 在路由执行前统一校验可信调用方白名单。
func (own *RouterInfo) AuthorizeInternalCaller(req IRequest) error {
	allowed := own.GetInternalCallers()
	if len(allowed) == 0 {
		return nil
	}
	callerRequest, ok := req.(IRequestInternalCaller)
	if !ok {
		return ErrInternalCallerForbidden
	}
	caller, trusted := callerRequest.TrustedInternalCaller()
	if !trusted {
		return ErrInternalCallerForbidden
	}
	for _, service := range allowed {
		if service == caller {
			return nil
		}
	}
	return ErrInternalCallerForbidden
}
