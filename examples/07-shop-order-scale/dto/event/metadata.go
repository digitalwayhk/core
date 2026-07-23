// Package event 定义 07 订单水平扩展示例跨服务事件的公共元数据 DTO。
package event

import "time"

// Metadata 定义所有事件载荷共享的追踪、来源和幂等信息。
type Metadata struct {
	SchemaVersion     int       `json:"schemaVersion"`
	EventID           string    `json:"eventID"`
	EventType         string    `json:"eventType"`
	Subject           string    `json:"subject"`
	TraceID           string    `json:"traceID"`
	ServiceName       string    `json:"serviceName"`
	ServiceInstanceID string    `json:"serviceInstanceID"`
	OccurredAt        time.Time `json:"occurredAt"`
}
