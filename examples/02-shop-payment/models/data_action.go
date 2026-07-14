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
	return ensureModelWith(getDataAction(), model)
}

// ensureModelWith 使用指定的数据操作器确保模型表已初始化。
func ensureModelWith(action persistencetypes.IDataAction, model interface{}) error {
	modelType := reflect.TypeOf(model)
	if modelType == nil || modelType.Kind() != reflect.Ptr {
		return NewBusinessError("模型类型无效")
	}
	result := reflect.New(reflect.SliceOf(modelType)).Interface()
	return action.Load(newSearch(model, 1), result)
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

// RunInTransaction 使用独立事务状态的适配器串行执行业务事务。
// operation 内的模型读写必须使用传入的 action，避免与普通非事务读写串扰。
func RunInTransaction(operation func(action persistencetypes.IDataAction) error) (err error) {
	transactionMu.Lock()
	defer transactionMu.Unlock()
	if err = EnsureStorage(); err != nil {
		return err
	}
	cloner, ok := getDataAction().(interface {
		Clone() persistencetypes.IDataAction
	})
	if !ok {
		return NewBusinessError("数据操作器不支持独立事务")
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
