// Package business 提供 07 订单本地 pending 的业务级 WriteBehindTarget。
package business

import (
	"context"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
)

// OrderWriteBehindTarget 将当前 order 副本的 Badger pending 汇合到共享 MySQL 权威库。
// 它只表达订单业务同步语义，pending ACK、重试保留和本地删除由 PrefixedBadgerDB 统一处理。
type OrderWriteBehindTarget struct {
	Remote RemoteOrderStore
}

// SyncBatch 将一批本地订单同步到远程权威库，并返回可 ACK 的 Badger key。
func (target OrderWriteBehindTarget) SyncBatch(ctx context.Context, items []*nosql.SyncQueueItem[models.Order]) (*nosql.WriteBehindResult, error) {
	remote := target.Remote
	if remote == nil {
		remote = ModelRemoteOrderStore{}
	}
	confirmed := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil || item.Item == nil || item.Key == "" {
			continue
		}
		if _, err := remote.Upsert(ctx, item.Item); err != nil {
			return nil, err
		}
		confirmed = append(confirmed, item.Key)
	}
	return &nosql.WriteBehindResult{ConfirmedKeys: confirmed}, nil
}
