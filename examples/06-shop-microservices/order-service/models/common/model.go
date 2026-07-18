// 本文件定义当前服务模型继承树的公共基础能力。
package common

import "github.com/digitalwayhk/core/pkg/persistence/entity"

// DatabaseName 提供本文件能力需要的导出定义。
const DatabaseName = "shop-order"

// OrderServiceModel 定义本文件能力使用的核心结构。
type OrderServiceModel struct {
	*entity.Model
	TraceID string `gorm:"index" json:"traceID"`
}

// NewOrderServiceModel 执行本文件能力对应的业务操作。
func NewOrderServiceModel() *OrderServiceModel {
	return &OrderServiceModel{Model: entity.NewModel()}
}

// GetLocalDBName 实现本类型在当前服务边界中的行为。
func (m *OrderServiceModel) GetLocalDBName() string { return DatabaseName }

// GetRemoteDBName 实现本类型在当前服务边界中的行为。
func (m *OrderServiceModel) GetRemoteDBName() string { return DatabaseName }
