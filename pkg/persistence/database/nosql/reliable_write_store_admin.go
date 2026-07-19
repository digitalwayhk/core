// Package nosql 提供与业务可靠写入门面分离的本地物理清理能力。
package nosql

import (
	"context"

	"github.com/digitalwayhk/core/pkg/persistence/types"
)

// ReliableWriteStoreAdmin 是 ReliableWriteStore 的独立运维 handle。
type ReliableWriteStoreAdmin[T types.IModel] struct {
	db *PrefixedBadgerDB[T]
}

// PurgeLocal 物理删除本地数据和 pending 索引，不产生远端删除语义。
func (admin *ReliableWriteStoreAdmin[T]) PurgeLocal(ctx context.Context, item *T) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if admin == nil || admin.db == nil || admin.db.IsClosed() {
		return ErrWriteStoreClosed
	}
	return admin.db.ForceDeleteLocal(item)
}
