// 本文件独立声明与 07 既有表兼容的业务字段，不引用 Core 模型。
package reference

import (
	"time"
)

// OutboxRecord 是原生对照服务直接交给 GORM 的记录。
type OutboxRecord struct {
	Record
	EventID     string     `gorm:"type:varchar(191);not null;uniqueIndex" json:"eventID"`
	EventType   string     `gorm:"index" json:"eventType"`
	Subject     string     `gorm:"index" json:"subject"`
	Payload     []byte     `json:"payload"`
	Published   bool       `gorm:"index" json:"published"`
	PublishedAt *time.Time `json:"publishedAt"`
}
