// Package store 封装 07 供应商服务统一数据访问和事务能力。
package store

import (
	"errors"
	"reflect"
	"sync"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models/common"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

var (
	actionOnce sync.Once
	action     persistencetypes.IDataAction
	txMu       sync.Mutex
	ensureMu   sync.Mutex
)

// Get 返回供应商服务本地权威库访问器。
func Get() persistencetypes.IDataAction {
	actionOnce.Do(func() { action = entity.GetGlobalSqliteInstance(common.LocalDatabaseName) })
	return action
}

// NewSearch 创建统一分页查询参数。
func NewSearch(model interface{}, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}

// EnsureModel 确保模型表已创建。
func EnsureModel(model interface{}) error {
	ensureMu.Lock()
	defer ensureMu.Unlock()
	t := reflect.TypeOf(model)
	if t == nil || t.Kind() != reflect.Ptr {
		return errors.New("模型类型无效")
	}
	return Get().Load(NewSearch(model, 1), reflect.New(reflect.SliceOf(t)).Interface())
}

// RunTransaction 在供应商服务本地权威库中串行执行事务。
func RunTransaction(ensureStorage func() error, operation func(persistencetypes.IDataAction) error) (err error) {
	txMu.Lock()
	defer txMu.Unlock()
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
