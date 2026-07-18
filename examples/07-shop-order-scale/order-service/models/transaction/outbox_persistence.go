// Package transaction 提供 07 订单服务 MySQL 权威 Outbox 的持久化能力。
package transaction

import (
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// PendingOutbox 读取尚未发布的 MySQL Outbox 事件。
func PendingOutbox(limit int) ([]*OutboxRecord, error) {
	var items []*OutboxRecord
	query := store.NewSearch(NewOutbox(), limit)
	query.AddWhereN("Published", false)
	query.AddSortN("ID", false)
	err := store.GetRemote().Load(query, &items)
	return items, err
}

// InsertOutboxIfMissingWith 幂等写入 Outbox，已存在相同 EventID 时直接成功。
func InsertOutboxIfMissingWith(action persistencetypes.IDataAction, item *OutboxRecord) error {
	if item == nil {
		return errors.New("Outbox 事件不能为空")
	}
	var items []*OutboxRecord
	query := store.NewSearch(NewOutbox(), 1)
	query.AddWhereN("EventID", strings.TrimSpace(item.EventID))
	if err := action.Load(query, &items); err != nil {
		return err
	}
	if len(items) > 0 {
		return nil
	}
	return item.InsertWith(action)
}

// MarkOutboxPublished 将 Outbox 事件标记为已发布。
func MarkOutboxPublished(item *OutboxRecord) error {
	return MarkOutboxPublishedWith(store.GetRemote(), item)
}

// MarkOutboxPublishedByID 按 Outbox 主键精确标记事件发布完成。
func MarkOutboxPublishedByID(action persistencetypes.IDataAction, id uint) error {
	var items []*OutboxRecord
	query := store.NewSearch(NewOutbox(), 1)
	query.AddWhereN("ID", id)
	if err := action.Load(query, &items); err != nil {
		return err
	}
	if len(items) == 0 {
		return errors.New("Outbox 事件不存在")
	}
	return MarkOutboxPublishedWith(action, items[0])
}

// MarkOutboxPublishedWith 在指定事务中将 Outbox 事件标记为已发布。
func MarkOutboxPublishedWith(action persistencetypes.IDataAction, item *OutboxRecord) error {
	now := time.Now().UTC()
	item.Published = true
	item.PublishedAt = &now
	return item.UpdateWith(action)
}
