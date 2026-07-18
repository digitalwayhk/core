// Package transaction 提供 07 订单服务本地 Outbox 的持久化能力。
package transaction

import (
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// PendingOutbox 读取尚未发布的本地 Outbox 事件。
func PendingOutbox(limit int) ([]*OutboxRecord, error) {
	var items []*OutboxRecord
	query := store.NewSearch(NewOutbox(), limit)
	query.AddWhereN("Published", false)
	query.AddSortN("ID", false)
	err := store.GetLocal().Load(query, &items)
	return items, err
}

// MarkOutboxPublished 将 Outbox 事件标记为已发布。
func MarkOutboxPublished(item *OutboxRecord) error {
	return MarkOutboxPublishedWith(store.GetLocal(), item)
}

// MarkOutboxPublishedWith 在指定事务中将 Outbox 事件标记为已发布。
func MarkOutboxPublishedWith(action persistencetypes.IDataAction, item *OutboxRecord) error {
	now := time.Now().UTC()
	item.Published = true
	item.PublishedAt = &now
	return item.UpdateWith(action)
}
