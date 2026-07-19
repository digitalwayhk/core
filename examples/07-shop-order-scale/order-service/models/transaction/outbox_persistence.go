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

// InsertOutboxesIfMissingWith 在当前事务中批量查询 EventID，并用一条批量 INSERT 写入缺失 Outbox。
func InsertOutboxesIfMissingWith(action persistencetypes.IDataAction, items []*OutboxRecord) error {
	if action == nil {
		return errors.New("数据操作器不能为空")
	}
	unique := make([]*OutboxRecord, 0, len(items))
	byHash := make(map[string]*OutboxRecord, len(items))
	hashes := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			return errors.New("Outbox 事件不能为空")
		}
		if strings.TrimSpace(item.EventID) == "" || strings.TrimSpace(item.Subject) == "" || len(item.Payload) == 0 {
			return errors.New("Outbox 事件参数不完整")
		}
		hash := item.GetHash()
		if _, ok := byHash[hash]; ok {
			continue
		}
		item.SetHashcode(hash)
		byHash[hash] = item
		unique = append(unique, item)
		hashes = append(hashes, hash)
	}
	if len(unique) == 0 {
		return nil
	}

	var existing []*OutboxRecord
	query := store.NewSearch(NewOutbox(), len(hashes))
	query.AddWhereNS("Hashcode", persistencetypes.SymbolIn, hashes)
	if err := action.Load(query, &existing); err != nil {
		return err
	}
	existingHashes := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		if item != nil {
			existingHashes[item.GetHash()] = struct{}{}
		}
	}
	missing := make([]*OutboxRecord, 0, len(unique))
	for _, item := range unique {
		if _, ok := existingHashes[item.GetHash()]; !ok {
			missing = append(missing, item)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return action.Insert(missing)
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
