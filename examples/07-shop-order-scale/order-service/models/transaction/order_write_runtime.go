// Package transaction 提供 07 订单服务实例级 typed 可靠写入 runtime。
// runtime 在路由构造期就作为稳定引用注入 API/business，Service.Start 稍后再绑定
// 当前副本 store，因此无需包级全局 registry，也不会让不同 Service 实例串用。
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
// 它只管理“业务当前能否访问 store”，store 的关闭 owner 仍是 ServiceContext 资源管理器。
type OrderWriteRuntime struct {
	mu    sync.RWMutex
	store *OrderWriteStore
}

// NewOrderWriteRuntime 创建尚未绑定 store 的实例级 runtime。
// 未绑定期间的业务写入会稳定返回 ErrOrderWriteStoreUnavailable，不会隐式初始化全局 store。
func NewOrderWriteRuntime() *OrderWriteRuntime { return &OrderWriteRuntime{} }

// Bind 绑定当前服务实例唯一的订单 store。
// 已绑定时拒绝静默 rebind，避免已构造的路由在运行中切换到生命周期不明的新 store。
func (runtime *OrderWriteRuntime) Bind(store *OrderWriteStore) error {
	// nil runtime/store 都表示 Service 装配未完成，不允许把空引用标记为已就绪。
	if runtime == nil || store == nil {
		return ErrOrderWriteStoreUnavailable
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	// 写锁使检查与赋值成为一个原子生命周期转换。
	if runtime.store != nil {
		return ErrOrderWriteStoreAlreadyBound
	}
	runtime.store = store
	return nil
}

// Unbind 断开业务入口与 store 的关联，不负责关闭资源。
// Service.Stop 和启动回滚使用它先阻断新业务访问；已注册资源仍由 ServiceContext 按逆序 Close。
func (runtime *OrderWriteRuntime) Unbind() {
	if runtime == nil {
		return
	}
	runtime.mu.Lock()
	runtime.store = nil
	runtime.mu.Unlock()
}

// Save 通过已绑定 store 可靠保存订单。
// 返回 nil 仍只代表当前副本本地可恢复，runtime 不改写 store 的 MySQL 汇合语义。
func (runtime *OrderWriteRuntime) Save(ctx context.Context, order *Order) error {
	return runtime.withStore(func(store *OrderWriteStore) error { return store.Save(ctx, order) })
}

// FindLocalByRequest 按当前实例本地用户幂等键查询订单。
// 未绑定时返回 ErrOrderWriteStoreUnavailable，已绑定时原样传递 store 的查询错误。
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
// runtime 不合并 MySQL 结果，远程权威查询仍由 business 查询层负责。
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
// 该委托保留 bounded sync 语义，不在 runtime 层循环到全量排空。
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
// runtime 未绑定时返回空快照，便于观测入口在启停窗口内安全采样。
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

// withStore 在完整委托调用期间持有读锁，保证 Stop 或启动回滚不会在业务调用进行到一半时 Unbind。
// 该锁只保护 runtime.store 引用的生命周期，store 内部并发安全仍由 ReliableWriteStore 负责。
func (runtime *OrderWriteRuntime) withStore(call func(*OrderWriteStore) error) error {
	if runtime == nil {
		return ErrOrderWriteStoreUnavailable
	}
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	// 在读锁内完成空值检查和委托，避免检查后、调用前被并发解绑。
	if runtime.store == nil {
		return ErrOrderWriteStoreUnavailable
	}
	return call(runtime.store)
}
