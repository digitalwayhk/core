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
