// Package models 提供 04 示例实例级订单可靠写入 runtime。
package models

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

var (
	// ErrOrderWriteStoreUnavailable 表示当前服务实例尚未绑定订单可靠 store。
	ErrOrderWriteStoreUnavailable = errors.New("订单可靠写入存储不可用")
	// ErrOrderWriteStoreAlreadyBound 表示 runtime 已绑定 store，拒绝静默替换。
	ErrOrderWriteStoreAlreadyBound = errors.New("订单可靠写入存储已绑定")
)

// OrderWriteRuntime 为业务和路由提供实例级 typed store 访问。
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

// DeleteAndSync 写入删除 tombstone，并在返回前确认 SQLite 已应用删除。
func (runtime *OrderWriteRuntime) DeleteAndSync(ctx context.Context, order *Order) error {
	return runtime.withStore(func(store *OrderWriteStore) error {
		if err := store.Delete(ctx, order); err != nil {
			return err
		}
		return store.Flush(ctx)
	})
}

// QueryVisibleOrders 合并 SQLite 已同步订单与 Badger pending 订单并按 ID 倒序去重。
func (runtime *OrderWriteRuntime) QueryVisibleOrders(ctx context.Context, userID string) ([]*Order, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, NewBusinessError("用户身份无效")
	}
	var result []*Order
	err := runtime.withStore(func(store *OrderWriteStore) error {
		persisted, err := NewOrder().QueryByUser(userID)
		if err != nil {
			return err
		}
		pending, err := store.PendingByUser(ctx, userID)
		if err != nil {
			return err
		}
		byID := make(map[uint]*Order, len(persisted)+len(pending))
		for _, order := range persisted {
			if order != nil {
				byID[order.ID] = order
			}
		}
		for _, order := range pending {
			if order != nil {
				byID[order.ID] = order
			}
		}
		result = make([]*Order, 0, len(byID))
		for _, order := range byID {
			result = append(result, order)
		}
		sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
		return nil
	})
	return result, err
}

// FlushPendingOrder 在 SQLite 事务前汇合指定本地订单。
func (runtime *OrderWriteRuntime) FlushPendingOrder(ctx context.Context, userID string, orderID uint) error {
	return runtime.withStore(func(store *OrderWriteStore) error {
		persisted, err := NewOrder().FindOwned(orderID, userID)
		if err != nil || persisted != nil {
			return err
		}
		pending, err := store.FindPendingOwned(ctx, userID, orderID)
		if err != nil || pending == nil {
			return err
		}
		return store.Flush(ctx)
	})
}

// FlushOrders 汇合当前实例全部订单，供事务和引用完整性检查使用。
func (runtime *OrderWriteRuntime) FlushOrders(ctx context.Context) error {
	return runtime.withStore(func(store *OrderWriteStore) error { return store.Flush(ctx) })
}

// Metrics 返回当前实例的订单写入性能快照。
func (runtime *OrderWriteRuntime) Metrics() (OrderWritePerformanceSnapshot, error) {
	var snapshot OrderWritePerformanceSnapshot
	err := runtime.withStore(func(store *OrderWriteStore) error {
		snapshot = store.PerformanceSnapshot()
		return nil
	})
	return snapshot, err
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
