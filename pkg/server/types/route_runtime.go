package types

import (
	"context"

	"github.com/digitalwayhk/core/pkg/server/event"
)

// RouteEventRuntime 是 RouterInfo 使用的最小事件运行时边界。
// ServiceContext 为同一服务内的所有路由注入同一个实现。
type RouteEventRuntime interface {
	Subscribe(eventType string, handler event.Handler) (func(), error)
	Publish(ctx context.Context, request event.PublishRequest) error
}
