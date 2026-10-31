// 本文件独立声明与既有事件线格式兼容的消息信封。
package reference

import "time"

type envelope struct {
	ID              string    `json:"id"`
	Source          string    `json:"source"`
	SpecVersion     string    `json:"specversion"`
	Type            string    `json:"type"`
	Subject         string    `json:"subject,omitempty"`
	Time            time.Time `json:"time"`
	DataContentType string    `json:"datacontenttype,omitempty"`
	Data            []byte    `json:"data,omitempty"`
	TraceID         string    `json:"traceid,omitempty"`
	IdempotencyKey  string    `json:"idempotencykey,omitempty"`
	ShardKey        string    `json:"shardkey,omitempty"`
}
