// 本文件定义 06 微服务示例事件通道使用的跨服务消息 DTO 能力。
package event

import "time"

// Metadata 定义本文件能力使用的核心结构。
type Metadata struct {
	EventID       string    `json:"eventID"`
	TraceID       string    `json:"traceID"`
	SchemaVersion int       `json:"schemaVersion"`
	EventType     string    `json:"eventType"`
	OccurredAt    time.Time `json:"occurredAt"`
	SourceService string    `json:"sourceService"`
	AggregateID   string    `json:"aggregateID"`
}
