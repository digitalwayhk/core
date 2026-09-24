// 本文件集中管理 Core 系统 Manage 模型的控制面数据源。
package smodels

import (
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetype "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/internal/managestore"
)

// NewManageModelList 为 Core 系统 Manage 创建使用统一控制面存储的模型列表。
func NewManageModelList[T persistencetype.IModel]() *entity.ModelList[T] {
	return entity.NewModelList[T](managestore.Clone())
}
