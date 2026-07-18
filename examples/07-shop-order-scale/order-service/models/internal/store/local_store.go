// Package store 封装 07 订单服务本地实例库的数据访问能力。
package store

import (
	"errors"
	"reflect"
	"sync"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/common"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

var (
	localActionOnce sync.Once
	localAction     persistencetypes.IDataAction
	localTxMu       sync.Mutex
	localEnsureMu   sync.Mutex
)

// GetLocal 返回当前 order 实例本地可靠写入库的数据访问器。
func GetLocal() persistencetypes.IDataAction {
	localActionOnce.Do(func() { localAction = entity.GetGlobalSqliteInstance(common.LocalDatabaseName) })
	return localAction
}

// NewSearch 创建统一分页查询参数。
func NewSearch(model interface{}, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}

// EnsureLocalModel 确保本地模型表已创建。
func EnsureLocalModel(model interface{}) error {
	return ensureModelWith(GetLocal(), &localEnsureMu, model)
}

// RunLocalTransaction 在本地实例库中串行执行事务。
func RunLocalTransaction(ensureStorage func() error, operation func(persistencetypes.IDataAction) error) error {
	return runTransaction(&localTxMu, &localEnsureMu, GetLocal, ensureStorage, operation)
}

func ensureModelWith(action persistencetypes.IDataAction, mu *sync.Mutex, model interface{}) error {
	mu.Lock()
	defer mu.Unlock()
	t := reflect.TypeOf(model)
	if t == nil || t.Kind() != reflect.Ptr {
		return errors.New("模型类型无效")
	}
	return action.Load(NewSearch(model, 1), reflect.New(reflect.SliceOf(t)).Interface())
}

func runTransaction(mu *sync.Mutex, ensureMu *sync.Mutex, getAction func() persistencetypes.IDataAction, ensureStorage func() error, operation func(persistencetypes.IDataAction) error) (err error) {
	mu.Lock()
	defer mu.Unlock()
	if err = ensureStorage(); err != nil {
		return err
	}
	ensureMu.Lock()
	cloner, ok := getAction().(interface {
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
