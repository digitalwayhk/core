// Package nosql 提供 ModelList/IDataAction 的 WriteBehindTarget 兼容层。
package nosql

import (
	"context"
	"errors"
	"fmt"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
)

// ModelListWriteBehindTarget 把旧 ModelList/IDataAction 写回模式适配为 WriteBehindTarget。
// 新业务热路径应优先提供业务专用 target；此类型用于 04 兼容迁移和低频通用模型写回。
type ModelListWriteBehindTarget[T types.IModel] struct {
	list *entity.ModelList[T]
}

// NewModelListWriteBehindTarget 创建 ModelList/IDataAction 兼容写回目标。
func NewModelListWriteBehindTarget[T types.IModel](list *entity.ModelList[T]) *ModelListWriteBehindTarget[T] {
	return &ModelListWriteBehindTarget[T]{list: list}
}

// SyncBatch 将 Badger pending 批次写入 ModelList 绑定的 IDataAction。
func (target *ModelListWriteBehindTarget[T]) SyncBatch(_ context.Context, items []*SyncQueueItem[T]) (*WriteBehindResult, error) {
	if target == nil || target.list == nil {
		return nil, errors.New("ModelListWriteBehindTarget 未绑定 ModelList")
	}
	if len(items) == 0 {
		return &WriteBehindResult{}, nil
	}
	action := target.dataAction(items[0])
	if action == nil {
		return nil, errors.New("ModelListWriteBehindTarget 未找到 IDataAction")
	}
	if _, err := action.GetModelDB(items[0].Item); err != nil {
		return nil, err
	}
	if err := action.Transaction(); err != nil {
		return nil, err
	}
	confirmed := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil || item.Item == nil || item.Key == "" {
			continue
		}
		setHashCode(item.Item)
		if err := target.apply(action, item); err != nil {
			_ = action.Rollback()
			return nil, err
		}
		confirmed = append(confirmed, item.Key)
	}
	if err := action.Commit(); err != nil {
		_ = action.Rollback()
		return nil, err
	}
	return &WriteBehindResult{ConfirmedKeys: confirmed}, nil
}

func (target *ModelListWriteBehindTarget[T]) dataAction(item *SyncQueueItem[T]) types.IDataAction {
	if target == nil || target.list == nil {
		return nil
	}
	var model *T
	if item != nil {
		model = item.Item
	}
	if model == nil {
		model = new(T)
		if hook, ok := any(model).(types.IModelNewHook); ok {
			hook.NewModel()
		}
	}
	searchItem := target.list.GetSearchItem()
	searchItem.Model = model
	action := target.list.GetDBAdapter(searchItem)
	if cloner, ok := action.(IActionCloner); ok {
		return cloner.Clone()
	}
	return action
}

func (target *ModelListWriteBehindTarget[T]) apply(action types.IDataAction, item *SyncQueueItem[T]) error {
	switch item.Op {
	case OpInsert:
		return action.Insert(item.Item)
	case OpUpdate:
		return action.Update(item.Item)
	case OpDelete:
		return action.Delete(item.Item)
	default:
		return fmt.Errorf("未知 write-behind 操作: %s", item.Op)
	}
}
