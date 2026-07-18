// 本文件封装当前服务模型层统一的数据访问和事务串行能力。
package store

import (
	"errors"
	"reflect"
	"sync"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/common"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

var (
	actionOnce    sync.Once
	action        persistencetypes.IDataAction
	transactionMu sync.Mutex
	ensureMu      sync.Mutex
)

// Get 执行本文件能力对应的业务操作。
func Get() persistencetypes.IDataAction {
	actionOnce.Do(func() { action = entity.GetGlobalSqliteInstance(common.DatabaseName) })
	return action
}

// NewSearch 执行本文件能力对应的业务操作。
func NewSearch(model interface{}, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}

// EnsureModel 执行本文件能力对应的业务操作。
func EnsureModel(model interface{}) error {
	return EnsureModelWith(Get(), model)
}

// EnsureModelWith 执行本文件能力对应的业务操作。
func EnsureModelWith(action persistencetypes.IDataAction, model interface{}) error {
	ensureMu.Lock()
	defer ensureMu.Unlock()
	t := reflect.TypeOf(model)
	if t == nil || t.Kind() != reflect.Ptr {
		return errors.New("模型类型无效")
	}
	return action.Load(NewSearch(model, 1), reflect.New(reflect.SliceOf(t)).Interface())
}

// Lock 执行本文件能力对应的业务操作。
func Lock() { ensureMu.Lock() }

// Unlock 执行本文件能力对应的业务操作。
func Unlock() { ensureMu.Unlock() }

// RunSerialized 执行本文件能力对应的业务操作。
func RunSerialized(operation func() error) error {
	transactionMu.Lock()
	defer transactionMu.Unlock()
	return operation()
}

// RunInTransaction 执行本文件能力对应的业务操作。
func RunInTransaction(ensureStorage func() error, operation func(persistencetypes.IDataAction) error) (err error) {
	transactionMu.Lock()
	defer transactionMu.Unlock()
	if err = ensureStorage(); err != nil {
		return err
	}
	ensureMu.Lock()
	cloner, ok := Get().(interface {
		Clone() persistencetypes.IDataAction
	})
	if !ok {
		ensureMu.Unlock()
		return errors.New("数据操作器不支持独立事务")
	}
	action := cloner.Clone()
	ensureMu.Unlock()
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
