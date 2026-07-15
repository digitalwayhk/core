package models

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// OrderWriteStore 使用 Badger 接收订单写入，再复用框架写后同步能力汇合到 SQLite。
// Add 无进程内互斥锁：唯一键为订单 ID（接口层 req.NewID），并发写依赖 Badger 自身。
type OrderWriteStore struct {
	db               *nosql.PrefixedBadgerDB[Order]
	batcher          *orderBatcher
	guard            *orderWriteGuard
	path             string
	startedAt        time.Time
	diskBytes        atomic.Int64
	diskScanFailures atomic.Uint64
	monitorStop      chan struct{}
	monitorDone      chan struct{}
	flushMu          sync.Mutex
	closeMu          sync.Mutex
	closed           bool
}

const (
	// orderCommitMaxBatch 限制单个 Badger 事务的订单数，避免过大事务增加内存和尾延迟。
	orderCommitMaxBatch = 128
	// orderCommitWait 是吞吐量与单请求延迟的平衡点：最多等待 1ms 收集同批订单。
	orderCommitWait = time.Millisecond
)

func newOrderWriteStore(path string, action persistencetypes.IDataAction, config nosql.BadgerDBConfig) (*OrderWriteStore, error) {
	db, err := nosql.NewSharedBadgerDB[Order](path, config)
	if err != nil {
		return nil, err
	}
	list := entity.NewModelList[Order](action)
	if err := db.EnableWriteBehind(list); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &OrderWriteStore{
		db:          db,
		guard:       newOrderWriteGuard(defaultOrderWriteGuardConfig()),
		path:        path,
		startedAt:   time.Now(),
		monitorStop: make(chan struct{}),
		monitorDone: make(chan struct{}),
	}
	// BatchInsert 使同批订单共享一次 SyncWrites 事务；每个 Submit 仍会等待该事务完成。
	store.batcher = newOrderBatcher(orderCommitMaxBatch, orderCommitWait, db.BatchInsert)
	store.refreshDiskUsage()
	go store.monitorDiskUsage()
	return store, nil
}

// Add 将订单持久写入本地 Badger；返回成功后请求即可完成。
// 无全局锁；调用方必须先为订单设置非零 ID（如接口层 req.NewID）。
func (s *OrderWriteStore) Add(order *Order) error {
	if order == nil {
		return NewValidationError("订单不能为空")
	}
	if order.GetID() == 0 {
		return NewValidationError("订单 ID 不能为空")
	}
	order.prepareForInsert()
	if key := order.GetHash(); key == "" {
		return NewValidationError("订单缓存键无效")
	}
	release, err := s.guard.Acquire(s.db.GetCachedPendingSyncCount(), s.diskBytes.Load(), time.Now())
	if err != nil {
		return err
	}
	defer release()
	return s.batcher.Submit(order)
}

// PendingByUser 返回尚在本地层可见的用户订单。
// Badger 本地键已编入可信 UserID 摘要，因此这里只扫描该用户前缀，不随全局积压量线性增长。
func (s *OrderWriteStore) PendingByUser(userID string) ([]*Order, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, NewBusinessError("用户身份无效")
	}
	items, err := s.db.Scan(orderPendingUserPrefix(userID), 0)
	if err != nil {
		return nil, err
	}
	result := make([]*Order, 0, len(items))
	for _, item := range items {
		if item != nil && strings.TrimSpace(item.UserID) == userID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	return result, nil
}

// FindPendingOwned 从本地层查找指定用户的订单。
func (s *OrderWriteStore) FindPendingOwned(userID string, orderID uint) (*Order, error) {
	items, err := s.PendingByUser(userID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item != nil && item.ID == orderID {
			return item, nil
		}
	}
	return nil, nil
}

// RemoveLocal 立即清除本地 Badger 事实与同步队列项，不产生远端删除操作。
// 必须在 SQLite 删除之前调用，避免 SQLite 已删而 pending 合并读把订单复活。
func (s *OrderWriteStore) RemoveLocal(order *Order) error {
	if order == nil {
		return nil
	}
	err := s.db.ForceDeleteLocal(order)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil
	}
	return err
}

// Flush 串行等待当前待同步订单写入 SQLite，避免多个事务入口重复冲刷同一批数据。
func (s *OrderWriteStore) Flush() error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	return s.db.ForceSyncAll()
}

// SyncStatus 返回积压、同步进度和关闭状态。
func (s *OrderWriteStore) SyncStatus() nosql.SyncStatus { return s.db.GetSyncStatus() }

// OrderWritePerformanceSnapshot 把 API 确认、Group Commit、SQLite 收敛与本地容量指标分开展示。
type OrderWritePerformanceSnapshot struct {
	Uptime                       time.Duration
	PendingOrders                int
	BadgerDiskBytes              int64
	DiskScanFailures             uint64
	LifetimeAPIConfirmedTPS      float64
	LifetimeSQLiteConvergenceTPS float64
	SQLiteActiveSyncTPS          float64
	GroupCommit                  OrderBatcherSnapshot
	Backpressure                 OrderWriteGuardSnapshot
	Sync                         nosql.SyncMetrics
}

// PerformanceSnapshot 返回当前进程生命周期的指标。
// LifetimeAPIConfirmedTPS 以 Badger 可靠提交为成功；
// LifetimeSQLiteConvergenceTPS 以最终同步条数为成功。两者都是含启动和空闲时间的进程生命周期均值。
func (s *OrderWriteStore) PerformanceSnapshot() OrderWritePerformanceSnapshot {
	if s == nil {
		return OrderWritePerformanceSnapshot{}
	}
	s.refreshDiskUsage()
	uptime := time.Since(s.startedAt)
	batch := s.batcher.Snapshot()
	syncMetrics := s.db.GetSyncMetrics()
	snapshot := OrderWritePerformanceSnapshot{
		Uptime:           uptime,
		PendingOrders:    s.db.GetCachedPendingSyncCount(),
		BadgerDiskBytes:  s.diskBytes.Load(),
		DiskScanFailures: s.diskScanFailures.Load(),
		GroupCommit:      batch,
		Backpressure:     s.guard.Snapshot(),
		Sync:             syncMetrics,
	}
	if seconds := uptime.Seconds(); seconds > 0 {
		snapshot.LifetimeAPIConfirmedTPS = float64(batch.CommittedOrders) / seconds
		snapshot.LifetimeSQLiteConvergenceTPS = float64(syncMetrics.SyncedItems) / seconds
	}
	if seconds := syncMetrics.TotalDuration.Seconds(); seconds > 0 {
		snapshot.SQLiteActiveSyncTPS = float64(syncMetrics.SyncedItems) / seconds
	}
	return snapshot
}

// GetOrderWritePerformanceSnapshot 供示例内的运维、调试或基准代码获取当前快照。
// 它不暴露 store 指针，避免外部绕过生命周期和背压约束。
func GetOrderWritePerformanceSnapshot() (OrderWritePerformanceSnapshot, error) {
	store, err := getOrderWriteStore()
	if err != nil {
		return OrderWritePerformanceSnapshot{}, err
	}
	return store.PerformanceSnapshot(), nil
}

func (s *OrderWriteStore) monitorDiskUsage() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	defer close(s.monitorDone)
	for {
		select {
		case <-ticker.C:
			s.refreshDiskUsage()
		case <-s.monitorStop:
			return
		}
	}
}

func (s *OrderWriteStore) refreshDiskUsage() {
	size, err := directorySize(s.path)
	if err != nil {
		s.diskScanFailures.Add(1)
		return
	}
	s.diskBytes.Store(size)
}

func directorySize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	return size, err
}

// Close 先尽力汇合积压，再关闭 Badger 和后台同步协程。
func (s *OrderWriteStore) Close(timeout time.Duration) error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.monitorStop)
	<-s.monitorDone
	batchErr := s.batcher.Close()
	flushErr := s.Flush()
	closeErr := s.db.CloseWithTimeout(timeout, timeout)
	return errors.Join(batchErr, flushErr, closeErr)
}

var (
	globalOrderWriteStoreMu sync.Mutex
	globalOrderWriteState   *orderWriteStoreState
	// activeOrderWriteStore 是请求热路径。Service.Start 完成后，每次下单只需一次原子读，
	// 不再为了获取长期存活的 store 反复竞争全局互斥锁。
	activeOrderWriteStore atomic.Pointer[OrderWriteStore]
)

type orderWriteStoreState struct {
	path  string
	once  sync.Once
	store *OrderWriteStore
	err   error
}

// StartOrderWriteStore 启动当前服务进程专属的订单写后同步存储。
func StartOrderWriteStore() error {
	globalOrderWriteStoreMu.Lock()
	defer globalOrderWriteStoreMu.Unlock()
	path := filepath.Join(utils.Getpath(), "data", "order-write-behind")
	if globalOrderWriteState != nil && globalOrderWriteState.path != path {
		activeOrderWriteStore.Store(nil)
		if globalOrderWriteState.store != nil {
			if err := globalOrderWriteState.store.Close(10 * time.Second); err != nil {
				return fmt.Errorf("关闭旧订单写入存储失败: %w", err)
			}
		}
		globalOrderWriteState = nil
	}
	if globalOrderWriteState == nil {
		globalOrderWriteState = &orderWriteStoreState{path: path}
	}
	state := globalOrderWriteState
	// 初始化错误由同一生命周期内的所有请求稳定观察；只有 Stop 才重置状态。
	state.once.Do(func() {
		if err := EnsureStorage(); err != nil {
			state.err = err
			return
		}
		config := nosql.DefaultProductionConfig(path)
		config.EnableLogger = false
		config.AutoSync = true
		config.SyncBatchDelay = 500 * time.Millisecond
		state.store, state.err = newOrderWriteStore(path, cloneDataAction(), config)
		if state.err == nil {
			activeOrderWriteStore.Store(state.store)
		}
	})
	return state.err
}

func getOrderWriteStore() (*OrderWriteStore, error) {
	if store := activeOrderWriteStore.Load(); store != nil {
		return store, nil
	}
	if err := StartOrderWriteStore(); err != nil {
		return nil, err
	}
	if store := activeOrderWriteStore.Load(); store != nil {
		return store, nil
	}
	// 仅保留初始化测试注入的慢路径；生产 Start 成功后必须走上面的原子读。
	globalOrderWriteStoreMu.Lock()
	defer globalOrderWriteStoreMu.Unlock()
	if globalOrderWriteState == nil || globalOrderWriteState.store == nil {
		return nil, errors.New("订单写入存储未初始化")
	}
	return globalOrderWriteState.store, nil
}

// StopOrderWriteStore 汇合积压并释放当前服务的本地订单存储。
func StopOrderWriteStore() error {
	globalOrderWriteStoreMu.Lock()
	defer globalOrderWriteStoreMu.Unlock()
	if globalOrderWriteState == nil {
		return nil
	}
	activeOrderWriteStore.Store(nil)
	var err error
	if globalOrderWriteState.store != nil {
		err = globalOrderWriteState.store.Close(10 * time.Second)
	}
	globalOrderWriteState = nil
	return err
}

// QueryVisibleOrders 合并 SQLite 已同步订单与 Badger 待同步订单，并按 ID 倒序去重。
func QueryVisibleOrders(userID string) ([]*Order, error) {
	persisted, err := NewOrder().QueryByUser(userID)
	if err != nil {
		return nil, err
	}
	store, err := getOrderWriteStore()
	if err != nil {
		return nil, err
	}
	pending, err := store.PendingByUser(userID)
	if err != nil {
		return nil, err
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
	result := make([]*Order, 0, len(byID))
	for _, order := range byID {
		result = append(result, order)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	return result, nil
}

// FlushPendingOrder 在需要 SQLite 事务处理前汇合指定本地订单。
func FlushPendingOrder(userID string, orderID uint) error {
	persisted, err := NewOrder().FindOwned(orderID, userID)
	if err != nil {
		return err
	}
	if persisted != nil {
		return nil
	}
	store, err := getOrderWriteStore()
	if err != nil {
		return err
	}
	pending, err := store.FindPendingOwned(userID, orderID)
	if err != nil || pending == nil {
		return err
	}
	return store.Flush()
}

// FlushOrders 汇合全部待同步订单，供引用完整性检查和服务关闭使用。
func FlushOrders() error {
	store, err := getOrderWriteStore()
	if err != nil {
		return err
	}
	return store.Flush()
}
