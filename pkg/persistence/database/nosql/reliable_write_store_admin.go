// Package nosql 提供与业务可靠写入门面分离的本地物理清理能力。
package nosql

import (
	"context"
	"errors"
	"github.com/dgraph-io/badger/v4"

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

// ReclaimStorage最多执行maxRounds次既有Badger原生GC，不删key、业务原件或顺序水位，不改变可靠刷盘。
// 这是共享物理库级运维操作，maxRounds为1..100；取消在原生单轮之间生效，不强杀正在执行的GC。
// 原生NoRewrite、已重写文件及实际磁盘/准入是否恢复分别核验；后台LSM压缩仍由Badger负责。
func (admin *ReliableWriteStoreAdmin[T]) ReclaimStorage(ctx context.Context, maxRounds int) (StorageReclaimResult, error) {
	var out StorageReclaimResult
	if admin == nil || admin.db == nil || admin.db.IsClosed() {
		return out, ErrWriteStoreClosed
	}
	if maxRounds < 1 || maxRounds > 100 {
		return out, ErrInvalidSyncLimit
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for range maxRounds {
		if e := ctx.Err(); e != nil {
			return out, e
		}
		out.Attempts++
		e := admin.db.manager.db.RunValueLogGC(admin.db.manager.config.GCDiscardRatio)
		if errors.Is(e, badger.ErrNoRewrite) {
			out.NoRewrite = true
			return out, nil
		}
		if e != nil {
			return out, e
		}
		out.RewrittenFiles++
	}
	return out, ctx.Err()
}
