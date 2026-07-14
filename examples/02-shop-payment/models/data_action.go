package models

import (
	"reflect"
	"sync"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

var (
	dataActionOnce sync.Once
	dataAction     persistencetypes.IDataAction
	transactionMu  sync.Mutex
)

// getDataAction 返回支付商城模型共享的数据操作接口。
func getDataAction() persistencetypes.IDataAction {
	dataActionOnce.Do(func() {
		dataAction = entity.GetGlobalSqliteInstance(NewProduct().GetLocalDBName())
	})
	return dataAction
}

// ensureModel 确保首次查询发生在模型表初始化之后。
func ensureModel(model interface{}) error {
	modelType := reflect.TypeOf(model)
	if modelType == nil || modelType.Kind() != reflect.Ptr {
		return NewBusinessError("模型类型无效")
	}
	result := reflect.New(reflect.SliceOf(modelType)).Interface()
	return getDataAction().Load(newSearch(model, 1), result)
}

// EnsureStorage 在事务开始前创建本示例使用的全部模型表。
func EnsureStorage() error {
	for _, model := range []interface{}{NewProduct(), NewOrder(), NewPaymentType(), NewPaymentRecord()} {
		if err := ensureModel(model); err != nil {
			return err
		}
	}
	return nil
}

// RunInTransaction 串行保护共享适配器的事务生命周期。
func RunInTransaction(operation func() error) (err error) {
	transactionMu.Lock()
	defer transactionMu.Unlock()
	if err = EnsureStorage(); err != nil {
		return err
	}
	action := getDataAction()
	if err = action.Transaction(); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = action.Rollback()
		}
	}()
	if err = operation(); err != nil {
		return err
	}
	if err = action.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
