package router

import "github.com/digitalwayhk/core/pkg/server/types"

// RouterInfoOption 只在创建尚未注册的 RouterInfo 时配置注册期元数据。
// RouterInfo 已由 ServiceContext 注册并冻结时，DefaultRouterInfoWithOptions 和
// NewRouterInfoWithOptions 会直接返回单例，不再执行任何 Option。
type RouterInfoOption interface {
	apply(*types.RouterInfo)
}

type routerInfoOptionFunc func(*types.RouterInfo)

func (f routerInfoOptionFunc) apply(info *types.RouterInfo) {
	f(info)
}

// WithMethod 设置路由注册使用的 HTTP 方法。
func WithMethod(method string) RouterInfoOption {
	return routerInfoOptionFunc(func(info *types.RouterInfo) {
		info.Method = method
	})
}

// WithPath 设置路由注册使用的完整路径。
func WithPath(path string) RouterInfoOption {
	return routerInfoOptionFunc(func(info *types.RouterInfo) {
		info.Path = path
	})
}

// WithPathResolver 根据刚创建的默认元数据计算完整路径。
// resolver 只在首次创建、Freeze 之前执行。
func WithPathResolver(resolver func(*types.RouterInfo) string) RouterInfoOption {
	return routerInfoOptionFunc(func(info *types.RouterInfo) {
		if resolver != nil {
			info.Path = resolver(info)
		}
	})
}

// WithAuth 设置路由是否需要认证。
func WithAuth(auth bool) RouterInfoOption {
	return routerInfoOptionFunc(func(info *types.RouterInfo) {
		info.Auth = auth
	})
}

// WithPathType 设置路由注册类型。
func WithPathType(pathType types.ApiType) RouterInfoOption {
	return routerInfoOptionFunc(func(info *types.RouterInfo) {
		info.PathType = pathType
	})
}

// WithPoolSize 设置路由注册后使用的对象池容量。
func WithPoolSize(size int) RouterInfoOption {
	return routerInfoOptionFunc(func(info *types.RouterInfo) {
		info.PoolSize = size
	})
}

// WithExternalRateLimit 为系统 Public API 配置每实例、每路由、每客户端 IP 限流。
func WithExternalRateLimit(rate float64, burst int) RouterInfoOption {
	return routerInfoOptionFunc(func(info *types.RouterInfo) {
		info.ConfigureExternalRateLimit(types.ExternalRateLimitPolicy{Rate: rate, Burst: burst})
	})
}

func applyRouterInfoOptions(info *types.RouterInfo, options []RouterInfoOption) {
	for _, option := range options {
		if option != nil {
			option.apply(info)
		}
	}
}
