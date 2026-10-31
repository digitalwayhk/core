// 本文件保存演示用技术fence与消费计数，不提供金融状态机或框架通用业务表。
package models

type ownerRecord struct {
	ID      uint   `gorm:"primaryKey"`
	Scope   string `gorm:"size:128;uniqueIndex:scope_key"`
	KeyName string `gorm:"size:128;uniqueIndex:scope_key"`
	Epoch   int64
	Token   string `gorm:"size:128"`
	Applied int64
}
