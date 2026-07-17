// Package runtime 提供示例 06 三个服务共用的无业务模型运行时。
package runtime

type OutboxRecord struct {
	ID                          uint
	EventID, EventType, Subject string
	Payload                     []byte
}
