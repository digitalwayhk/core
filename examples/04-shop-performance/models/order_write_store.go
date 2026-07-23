// Package models 提供 04 示例订单到框架 ReliableWriteStore 的领域适配。
package models

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// OrderWriteStore 负责订单校验、用户维度本地查询和 SQLite 写回目标适配。
type OrderWriteStore struct {
	reliable *nosql.ReliableWriteStore[Order]
	flushMu  sync.Mutex
}

// NewOrderWriteStore 创建实例隔离的订单可靠写入适配器。
func NewOrderWriteStore(
	identity nosql.ServiceIdentity,
	action persistencetypes.IDataAction,
	config nosql.ReliableWriteStoreConfig,
) (*OrderWriteStore, error) {
	reliable, _, err := nosql.NewReliableWriteStore[Order](identity, config)
	if err != nil {
		return nil, err
	}
	target := nosql.NewModelListWriteBehindTarget(entity.NewModelList[Order](action))
	if err := reliable.UseWriteBehind(target); err != nil {
		_ = reliable.Close(context.Background())
		return nil, err
	}
	return &OrderWriteStore{reliable: reliable}, nil
}

// Save 校验并可靠保存订单；返回成功只表示本地提交完成。
func (store *OrderWriteStore) Save(ctx context.Context, order *Order) error {
	if order == nil {
		return NewValidationError("订单不能为空")
	}
	if order.GetID() == 0 {
		return NewValidationError("订单 ID 不能为空")
	}
	order.prepareForInsert()
	if order.GetHash() == "" || order.GetLocalKey() == "" {
		return NewValidationError("订单缓存键无效")
	}
	return store.reliable.Save(ctx, order)
}

// Delete 为 SQLite 已存在的订单可靠生成远端删除 tombstone。
func (store *OrderWriteStore) Delete(ctx context.Context, order *Order) error {
	if order == nil {
		return NewValidationError("订单不能为空")
	}
	// IsSyncAfterDelete 会在每次 ACK 后物理清理本地副本；先恢复快照，才能让
	// 通用层的幂等 Delete 对远端已存在订单生成明确 tombstone。
	if err := store.Save(ctx, order); err != nil {
		return err
	}
	return store.reliable.Delete(ctx, order)
}

// PendingByUser 返回本地层仍可见的指定用户订单。
func (store *OrderWriteStore) PendingByUser(ctx context.Context, userID string) ([]*Order, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, NewBusinessError("用户身份无效")
	}
	items, err := store.reliable.ScanLocal(ctx, nosql.LocalScanOptions{Prefix: orderPendingUserPrefix(userID)})
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
func (store *OrderWriteStore) FindPendingOwned(ctx context.Context, userID string, orderID uint) (*Order, error) {
	items, err := store.PendingByUser(ctx, userID)
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

// Flush 串行汇合当前实例的全部 pending 订单。
func (store *OrderWriteStore) Flush(ctx context.Context) error {
	store.flushMu.Lock()
	defer store.flushMu.Unlock()
	_, err := store.reliable.ForceSyncAll(ctx)
	return err
}

// OrderWritePerformanceSnapshot 映射框架本地提交、背压、磁盘和同步指标。
type OrderWritePerformanceSnapshot struct {
	Uptime                       time.Duration
	PendingOrders                int
	BadgerDiskBytes              int64
	LifetimeAPIConfirmedTPS      float64
	LifetimeSQLiteConvergenceTPS float64
	SQLiteActiveSyncTPS          float64
	GroupCommit                  nosql.BatchCommitMetrics
	Backpressure                 nosql.WriteAdmissionMetrics
	Sync                         nosql.SyncMetrics
}

// PerformanceSnapshot 返回当前实例的只读性能快照。
func (store *OrderWriteStore) PerformanceSnapshot() OrderWritePerformanceSnapshot {
	if store == nil || store.reliable == nil {
		return OrderWritePerformanceSnapshot{}
	}
	metrics := store.reliable.Metrics()
	uptime := time.Since(metrics.StartedAt)
	snapshot := OrderWritePerformanceSnapshot{
		Uptime:          uptime,
		PendingOrders:   metrics.Pending,
		BadgerDiskBytes: metrics.BadgerLSMBytes + metrics.BadgerVLogBytes,
		GroupCommit:     metrics.Batch,
		Backpressure:    metrics.Admission,
		Sync:            metrics.Sync,
	}
	if seconds := uptime.Seconds(); seconds > 0 {
		snapshot.LifetimeAPIConfirmedTPS = float64(metrics.Batch.Committed) / seconds
		snapshot.LifetimeSQLiteConvergenceTPS = float64(metrics.Sync.SyncedItems) / seconds
	}
	if seconds := metrics.Sync.TotalDuration.Seconds(); seconds > 0 {
		snapshot.SQLiteActiveSyncTPS = float64(metrics.Sync.SyncedItems) / seconds
	}
	return snapshot
}

// Close 排空已接收的本地提交并关闭当前订单 prefix，不强制访问 SQLite。
func (store *OrderWriteStore) Close(ctx context.Context) error {
	if store == nil || store.reliable == nil {
		return nil
	}
	return store.reliable.Close(ctx)
}
