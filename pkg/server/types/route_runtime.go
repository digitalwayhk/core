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
