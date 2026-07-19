// Package transaction 提供 07 订单服务实例级 typed 可靠写入 runtime。
package transaction

import (
	"context"
	"errors"
	"sync"

	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
)

var (
	// ErrOrderWriteStoreAlreadyBound 表示 runtime 已绑定 store，拒绝静默替换。
	ErrOrderWriteStoreAlreadyBound = errors.New("订单可靠写入存储已绑定")
)

// OrderWriteRuntime 为路由与业务提供实例级 OrderWriteAccess。
type OrderWriteRuntime struct {
	mu    sync.RWMutex
	store *OrderWriteStore
}

// NewOrderWriteRuntime 创建尚未绑定 store 的实例级 runtime。
func NewOrderWriteRuntime() *OrderWriteRuntime { return &OrderWriteRuntime{} }

// Bind 绑定当前服务实例唯一的订单 store。
func (runtime *OrderWriteRuntime) Bind(store *OrderWriteStore) error {
	if runtime == nil || store == nil {
		return ErrOrderWriteStoreUnavailable
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.store != nil {
		return ErrOrderWriteStoreAlreadyBound
	}
	runtime.store = store
	return nil
}

// Unbind 断开业务入口与 store 的关联，不负责关闭资源。
func (runtime *OrderWriteRuntime) Unbind() {
	if runtime == nil {
		return
	}
	runtime.mu.Lock()
	runtime.store = nil
	runtime.mu.Unlock()
}

// Save 通过已绑定 store 可靠保存订单。
func (runtime *OrderWriteRuntime) Save(ctx context.Context, order *Order) error {
	return runtime.withStore(func(store *OrderWriteStore) error { return store.Save(ctx, order) })
}

// FindLocalByRequest 按当前实例本地用户幂等键查询订单。
func (runtime *OrderWriteRuntime) FindLocalByRequest(
	ctx context.Context,
	userID uint,
	requestID string,
) (*Order, error) {
	var result *Order
	err := runtime.withStore(func(store *OrderWriteStore) error {
		var err error
		result, err = store.FindLocalByRequest(ctx, userID, requestID)
		return err
	})
	return result, err
}

// PendingByUser 返回当前实例本地可见的用户订单。
func (runtime *OrderWriteRuntime) PendingByUser(ctx context.Context, userID uint) ([]*Order, error) {
	var result []*Order
	err := runtime.withStore(func(store *OrderWriteStore) error {
		var err error
		result, err = store.PendingByUser(ctx, userID)
		return err
	})
	return result, err
}

// ForceSyncBatch 最多同步 limit 条当前实例 pending。
func (runtime *OrderWriteRuntime) ForceSyncBatch(
	ctx context.Context,
	limit int,
) (nosql.ForceSyncResult, error) {
	var result nosql.ForceSyncResult
	err := runtime.withStore(func(store *OrderWriteStore) error {
		var err error
		result, err = store.ForceSyncBatch(ctx, limit)
		return err
	})
	return result, err
}

// Metrics 返回当前实例可靠写入指标。
func (runtime *OrderWriteRuntime) Metrics() nosql.ReliableWriteMetrics {
	if runtime == nil {
		return nosql.ReliableWriteMetrics{}
	}
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	if runtime.store == nil {
		return nosql.ReliableWriteMetrics{}
	}
	return runtime.store.Metrics()
}

func (runtime *OrderWriteRuntime) withStore(call func(*OrderWriteStore) error) error {
	if runtime == nil {
		return ErrOrderWriteStoreUnavailable
	}
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	if runtime.store == nil {
		return ErrOrderWriteStoreUnavailable
	}
	return call(runtime.store)
}
