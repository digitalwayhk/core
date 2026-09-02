package models

import (
	"sync"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

var (
	dataActionOnce sync.Once
	dataAction     persistencetypes.IDataAction
)

// getDataAction 返回商城模型共享的数据操作接口。
// 数据库实现的选择集中在模型持久化边界，不向 Service 或 API 路由传递。
func getDataAction() persistencetypes.IDataAction {
	dataActionOnce.Do(func() {
		dataAction = entity.GetGlobalSqliteInstance(NewProduct().GetLocalDBName())
	})
	return dataAction
}

// NewManageModelList 为当前服务的 Manage API 创建模型列表。
//
// 本方法是 Manage 访问 ModelList 的唯一 models 层入口。调用方只声明要管理的
// 模型类型，不得感知或传入 IDataAction，也不得决定数据库位置。连接类型、
// 数据库位置和路由策略全部由当前服务的 models 持久化组合根集中选择。
func NewManageModelList[T persistencetypes.IModel]() *entity.ModelList[T] {
	return entity.NewModelList[T](getDataAction())
}
