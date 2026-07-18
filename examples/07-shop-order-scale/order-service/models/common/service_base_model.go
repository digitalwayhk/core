// Package common 定义 07 订单服务所有模型继承的服务级基础模型。
package common

import "github.com/digitalwayhk/core/pkg/persistence/entity"

// ServiceBaseModel 承载订单服务模型的数据库名、追踪 ID 和服务名。
type ServiceBaseModel struct {
	*entity.Model
	TraceID     string `gorm:"index" json:"traceID"`
	ServiceName string `gorm:"index" json:"serviceName"`
}

// NewServiceBaseModel 创建已初始化嵌入模型的服务级基础模型。
func NewServiceBaseModel() *ServiceBaseModel {
	return &ServiceBaseModel{Model: entity.NewModel(), ServiceName: "shop-order"}
}

// GetLocalDBName 返回当前 order 实例本地可靠写入库名。
func (m *ServiceBaseModel) GetLocalDBName() string { return LocalDatabaseName }

// GetRemoteDBName 返回所有 order 实例共享的远程权威库名。
func (m *ServiceBaseModel) GetRemoteDBName() string { return RemoteDatabaseName }
