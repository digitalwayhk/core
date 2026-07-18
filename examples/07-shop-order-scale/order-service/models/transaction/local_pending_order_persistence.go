// Package transaction 提供 07 订单服务本地 pending 的持久化能力。
package transaction

import (
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// FindLocalPendingByRequest 按 UserID + requestID 查找本地 pending。
func FindLocalPendingByRequest(userID uint, requestID string) (*LocalPendingOrder, error) {
	var items []*LocalPendingOrder
	query := store.NewSearch(NewLocalPendingOrder(), 1)
	query.AddWhereN("UserID", userID)
	query.AddWhereN("RequestID", strings.TrimSpace(requestID))
	if err := store.GetLocal().Load(query, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("本地 pending 订单不存在")
	}
	return items[0], nil
}

// PendingLocalOrders 读取尚未同步成功的本地 pending。
func PendingLocalOrders(limit int) ([]*LocalPendingOrder, error) {
	var items []*LocalPendingOrder
	query := store.NewSearch(NewLocalPendingOrder(), limit)
	query.AddWhereN("SyncStatus", PendingStatusAccepted)
	query.AddSortN("ID", false)
	if err := store.GetLocal().Load(query, &items); err != nil {
		return nil, err
	}
	var failed []*LocalPendingOrder
	failedQuery := store.NewSearch(NewLocalPendingOrder(), limit)
	failedQuery.AddWhereN("SyncStatus", PendingStatusFailed)
	failedQuery.AddSortN("ID", false)
	if err := store.GetLocal().Load(failedQuery, &failed); err != nil {
		return nil, err
	}
	items = append(items, failed...)
	if len(items) > limit {
		return items[:limit], nil
	}
	return items, nil
}

// MarkPendingSyncedWith 在指定事务中把本地 pending 标记为已同步。
func MarkPendingSyncedWith(action persistencetypes.IDataAction, pending *LocalPendingOrder) error {
	now := time.Now().UTC()
	pending.SyncStatus = PendingStatusSynced
	pending.LastError = ""
	pending.SyncedAt = &now
	return pending.UpdateWith(action)
}

// MarkPendingFailedWith 在指定事务中记录本地 pending 同步失败。
func MarkPendingFailedWith(action persistencetypes.IDataAction, pending *LocalPendingOrder, message string) error {
	pending.SyncStatus = PendingStatusFailed
	pending.RetryCount++
	pending.LastError = strings.TrimSpace(message)
	return pending.UpdateWith(action)
}
