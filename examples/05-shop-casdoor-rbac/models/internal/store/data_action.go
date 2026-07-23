package store

import (
	"reflect"
	"sync"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/common"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

var (
	dataActionOnce sync.Once
	dataAction     persistencetypes.IDataAction
	transactionMu  sync.Mutex
)

// Get 返回本示例所有模型共享的数据操作器。
func Get() persistencetypes.IDataAction {
	dataActionOnce.Do(func() {
		dataAction = entity.GetGlobalSqliteInstance(common.DatabaseName)
	})
	return dataAction
}

// NewSearch 创建模型查询的统一分页条件。
func NewSearch(model interface{}, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}

// EnsureModel 确保首次查询发生在模型表初始化之后。
func EnsureModel(model interface{}) error {
	return EnsureModelWith(Get(), model)
}

// EnsureModelWith 使用指定数据操作器确保模型表已初始化。
func EnsureModelWith(action persistencetypes.IDataAction, model interface{}) error {
	modelType := reflect.TypeOf(model)
	if modelType == nil || modelType.Kind() != reflect.Ptr {
		return common.NewBusinessError("模型类型无效")
	}
	result := reflect.New(reflect.SliceOf(modelType)).Interface()
	return action.Load(NewSearch(model, 1), result)
}

// RunInTransaction 使用独立事务适配器串行执行业务事务。
func RunInTransaction(
	ensureStorage func() error,
	operation func(action persistencetypes.IDataAction) error,
) (err error) {
	transactionMu.Lock()
	defer transactionMu.Unlock()
	if err = ensureStorage(); err != nil {
		return err
	}
	cloner, ok := Get().(interface {
		Clone() persistencetypes.IDataAction
	})
	if !ok {
		return common.NewBusinessError("数据操作器不支持独立事务")
	}
	action := cloner.Clone()
	if err = action.Transaction(); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = action.Rollback()
		}
	}()
	if err = operation(action); err != nil {
		return err
	}
	if err = action.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
