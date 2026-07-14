package types

import (
	"context"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
)

// RouteEventRuntime 是 RouterInfo 使用的最小事件运行时边界。
// ServiceContext 为同一服务内的所有路由注入同一个实现。
type RouteEventRuntime interface {
	Subscribe(eventType string, handler event.Handler) (func(), error)
	Publish(ctx context.Context, request event.PublishRequest) error
}

// RouteCacheRuntime 是 RouterInfo 使用的最小缓存运行时边界。
type RouteCacheRuntime interface {
	EnableRoute(route string, ttl time.Duration) error
	Get(route string, source interface{}) (interface{}, bool, error)
	Set(route string, source, value interface{}, ttl time.Duration) error
	Delete(route string, source interface{}) error
	DeleteRoute(route string) error
}

// RouteCacheTakeRuntime 是可选的缓存加载合并能力。
//
// RouterInfo 仅在运行时实现本接口时使用它；已有自定义 RouteCacheRuntime 无需修改。
// 实现必须只返回 loader 的业务错误，缓存读写错误应按 best-effort 旁路处理，不能把
// 一个已经成功计算出的业务结果改成失败。
type RouteCacheTakeRuntime interface {
	TakeBestEffort(route string, source interface{}, ttl time.Duration, loader func() (interface{}, error)) (interface{}, error)
}
