// Package nosql 提供面向 SQL 权威库的通用 WriteBehindTarget。
package nosql

import (
	"context"
	"errors"

	"github.com/digitalwayhk/core/pkg/persistence/types"
)

// SQLWriteBehindStore 定义 SQL 权威库批量写入所需的最小能力。
// 具体实现可以使用 GORM、database/sql、MySQL upsert 或业务自定义事务。
type SQLWriteBehindStore[T types.IModel] interface {
	UpsertBatch(ctx context.Context, items []*T) ([]*T, error)
	DeleteBatch(ctx context.Context, items []*T) error
}

// SQLWriteBehindTarget 把 PrefixedBadgerDB pending 批次同步到 SQL store。
type SQLWriteBehindTarget[T types.IModel] struct {
	store SQLWriteBehindStore[T]
}

// NewSQLWriteBehindTarget 创建 SQL 写回目标。
func NewSQLWriteBehindTarget[T types.IModel](store SQLWriteBehindStore[T]) *SQLWriteBehindTarget[T] {
	return &SQLWriteBehindTarget[T]{store: store}
}

// SyncBatch 按操作类型分组调用 SQL store，成功后确认所有已处理 key。
func (target *SQLWriteBehindTarget[T]) SyncBatch(ctx context.Context, items []*SyncQueueItem[T]) (*WriteBehindResult, error) {
	if target == nil || target.store == nil {
		return nil, errors.New("SQLWriteBehindTarget 未绑定 store")
	}
	if len(items) == 0 {
		return &WriteBehindResult{}, nil
	}
	upserts := make([]*T, 0, len(items))
	deletes := make([]*T, 0)
	upsertKeys := make([]string, 0, len(items))
	deleteKeys := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil || item.Item == nil || item.Key == "" {
			continue
		}
		switch item.Op {
		case OpInsert, OpUpdate:
			upserts = append(upserts, item.Item)
			upsertKeys = append(upsertKeys, item.Key)
		case OpDelete:
			deletes = append(deletes, item.Item)
			deleteKeys = append(deleteKeys, item.Key)
		}
	}
	if len(upserts) > 0 {
		if _, err := target.store.UpsertBatch(ctx, upserts); err != nil {
			return nil, err
		}
	}
	if len(deletes) > 0 {
		if err := target.store.DeleteBatch(ctx, deletes); err != nil {
			return &WriteBehindResult{ConfirmedKeys: upsertKeys}, err
		}
	}
	confirmed := append(upsertKeys, deleteKeys...)
	return &WriteBehindResult{ConfirmedKeys: confirmed}, nil
}
