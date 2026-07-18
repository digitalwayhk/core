// Package common 定义 07 订单服务用于水平扩展排查的运行时戳模型。
package common

// RuntimeStampedModel 为 pending、Outbox、Inbox 和投影记录运行实例信息。
type RuntimeStampedModel struct {
	*ServiceBaseModel
	ServiceInstanceID string `gorm:"index" json:"serviceInstanceID"`
	ServiceInstanceIP string `json:"serviceInstanceIP"`
}

// NewRuntimeStampedModel 创建带服务基础模型的运行时戳模型。
func NewRuntimeStampedModel() *RuntimeStampedModel {
	return &RuntimeStampedModel{ServiceBaseModel: NewServiceBaseModel()}
}
