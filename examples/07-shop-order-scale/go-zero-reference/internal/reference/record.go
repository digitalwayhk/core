// 本文件定义原生服务的通用持久字段，兼容既有测试 schema。
package reference

import "time"

// Record 保存数据库公共字段，不嵌入框架模型。
type Record struct {
	ID                uint       `gorm:"primaryKey;autoIncrement:false" json:"id"`
	CreatedAt         *time.Time `json:"createdAt"`
	UpdatedAt         *time.Time `json:"updatedAt"`
	Hashcode          string     `gorm:"column:hashcode" json:"hashCode"`
	TraceID           string     `json:"traceID"`
	ServiceName       string     `json:"serviceName"`
	ServiceInstanceID string     `json:"serviceInstanceID"`
	ServiceInstanceIP string     `json:"serviceInstanceIP"`
}
