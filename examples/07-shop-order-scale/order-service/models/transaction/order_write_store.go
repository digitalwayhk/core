// Package transaction 提供 07 订单服务的专用业务写入存储。
package transaction

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
)

const (
	orderCommitMaxBatch = 128
	orderCommitWait     = time.Millisecond
)

// OrderWriteStore 使用 Badger 承接订单热路径写入，再由业务同步器汇合到 MySQL。
type OrderWriteStore struct {
	db               *nosql.PrefixedBadgerDB[Order]
	batcher          *orderBatcher
	guard            *orderWriteGuard
	path             string
	startedAt        time.Time
	pendingCount     atomic.Int64
	diskBytes        atomic.Int64
	diskScanFailures atomic.Uint64
	monitorStop      chan struct{}
	monitorDone      chan struct{}
	closeMu          sync.Mutex
	closed           bool
}

func newOrderWriteStore(path string, config nosql.BadgerDBConfig) (*OrderWriteStore, error) {
	db, err := nosql.NewSharedBadgerDB[Order](path, config)
	if err != nil {
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
	store.batcher = newOrderBatcher(orderCommitMaxBatch, orderCommitWait, db.BatchInsert)
	store.refreshPendingCount()
	store.refreshDiskUsage()
	go store.monitorDiskUsage()
	return store, nil
}

// Add 将订单可靠提交到当前实例本地 Badger。
func (s *OrderWriteStore) Add(order *Order) error {
	if order == nil {
		return errors.New("订单不能为空")
	}
	if order.GetID() == 0 {
		return errors.New("订单 ID 不能为空")
	}
	if err := order.validate(); err != nil {
		return err
	}
	order.prepareForLocalInsert()
	release, err := s.guard.Acquire(s.PendingCount(), s.diskBytes.Load(), time.Now())
	if err != nil {
		return err
	}
	defer release()
	if err := s.batcher.Submit(order); err != nil {
		return err
	}
	s.pendingCount.Add(1)
	return nil
}

// PendingByUser 返回本地尚未汇合到 MySQL 的指定用户订单。
func (s *OrderWriteStore) PendingByUser(userID uint) ([]*Order, error) {
	items, err := s.db.Scan(OrderPendingUserPrefix(userID), 0)
	if err != nil {
		return nil, err
	}
	result := make([]*Order, 0, len(items))
	for _, item := range items {
		if item != nil && item.UserID == userID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	return result, nil
}

// FindPendingByRequest 按用户幂等键查找本地订单。
func (s *OrderWriteStore) FindPendingByRequest(userID uint, requestID string) (*Order, error) {
	items, err := s.PendingByUser(userID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item != nil && item.RequestID == requestID {
			return item, nil
		}
	}
	return nil, nil
}

// PendingOrders 返回当前实例待汇合的本地订单批次。
func (s *OrderWriteStore) PendingOrders(limit int) ([]*Order, error) {
	return s.db.Scan("", limit)
}

// RemoveLocal 删除已成功汇合或不再需要的本地订单。
func (s *OrderWriteStore) RemoveLocal(order *Order) error {
	if order == nil {
		return nil
	}
	err := s.db.ForceDeleteLocal(order)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil
	}
	if err == nil {
		s.pendingCount.Add(-1)
	}
	return err
}

// PendingCount 返回当前实例本地待汇合订单数量。
func (s *OrderWriteStore) PendingCount() int {
	value := s.pendingCount.Load()
	if value < 0 {
		return 0
	}
	return int(value)
}

func (s *OrderWriteStore) refreshPendingCount() {
	items, err := s.PendingOrders(0)
	if err != nil {
		s.diskScanFailures.Add(1)
		return
	}
	s.pendingCount.Store(int64(len(items)))
}

// OrderWritePerformanceSnapshot 展示本地接单和同步积压状态。
type OrderWritePerformanceSnapshot struct {
	Uptime                  time.Duration
	PendingOrders           int
	BadgerDiskBytes         int64
	DiskScanFailures        uint64
	LifetimeAPIConfirmedTPS float64
	GroupCommit             OrderBatcherSnapshot
	Backpressure            OrderWriteGuardSnapshot
}

// PerformanceSnapshot 返回本实例订单写入存储的运行快照。
func (s *OrderWriteStore) PerformanceSnapshot() OrderWritePerformanceSnapshot {
	if s == nil {
		return OrderWritePerformanceSnapshot{}
	}
	s.refreshDiskUsage()
	uptime := time.Since(s.startedAt)
	batch := s.batcher.Snapshot()
	snapshot := OrderWritePerformanceSnapshot{
		Uptime:           uptime,
		PendingOrders:    s.PendingCount(),
		BadgerDiskBytes:  s.diskBytes.Load(),
		DiskScanFailures: s.diskScanFailures.Load(),
		GroupCommit:      batch,
		Backpressure:     s.guard.Snapshot(),
	}
	if seconds := uptime.Seconds(); seconds > 0 {
		snapshot.LifetimeAPIConfirmedTPS = float64(batch.CommittedOrders) / seconds
	}
	return snapshot
}

// Close 停止接收新订单并关闭 Badger。
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
	closeErr := s.db.CloseWithTimeout(timeout, timeout)
	return errors.Join(batchErr, closeErr)
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
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return size, err
}

func (o *Order) prepareForLocalInsert() {
	now := time.Now().UTC().Truncate(time.Second)
	if o.CreatedAt != nil {
		now = o.CreatedAt.UTC().Truncate(time.Second)
	}
	o.SetCreatedAt(now)
	o.SetUpdatedAt(now)
	o.SetHashcode(o.GetHash())
	if o.AcceptedAt.IsZero() {
		o.AcceptedAt = now
	}
}
