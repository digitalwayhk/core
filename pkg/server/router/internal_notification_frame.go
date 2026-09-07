// 本文件定义只在框架内部广播通道使用的带版本消息信封。
package router

import "github.com/digitalwayhk/core/pkg/server/event"

type internalNotificationFrame struct {
	Version  int             `json:"version"`
	Origin   string          `json:"origin"`
	Envelope *event.Envelope `json:"envelope"`
}
