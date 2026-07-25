// Package common 定义 07 供应商服务所有模型继承的服务级基础模型。
package common

import "github.com/digitalwayhk/core/pkg/persistence/entity"

// ServiceBaseModel 承载供应商服务模型的数据库名和 TraceID。
type ServiceBaseModel struct {
	*entity.Model
	TraceID string `gorm:"index" json:"traceID"`
}

// NewServiceBaseModel 创建供应商服务基础模型。
func NewServiceBaseModel() *ServiceBaseModel {
	return &ServiceBaseModel{Model: entity.NewModel()}
}

// GetLocalDBName 返回供应商服务本地库名。
func (m *ServiceBaseModel) GetLocalDBName() string { return LocalDatabaseName }

// GetRemoteDBName 返回供应商服务远程库名；示例中与本地权威库一致。
func (m *ServiceBaseModel) GetRemoteDBName() string { return LocalDatabaseName }
