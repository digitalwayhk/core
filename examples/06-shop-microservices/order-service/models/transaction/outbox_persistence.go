// 本文件定义当前服务交易事实、Outbox、Inbox 或投影模型能力。
package transaction

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// NewOutboxRecord 执行本文件能力对应的业务操作。
func NewOutboxRecord(traceID, eventID, eventType, subject string, payload interface{}) (*Outbox, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	outbox := NewOutbox()
	outbox.TraceID = strings.TrimSpace(traceID)
	outbox.EventID, outbox.EventType, outbox.Subject, outbox.Payload = strings.TrimSpace(eventID), eventType, subject, data
	outbox.SetHashcode(outbox.GetHash())
	return outbox, nil
}

// PendingOutbox 执行本文件能力对应的业务操作。
func PendingOutbox() ([]*Outbox, error) {
	if err := store.EnsureModel(NewOutbox()); err != nil {
		return nil, err
	}
	var items []*Outbox
	query := store.NewSearch(NewOutbox(), 100)
	query.AddWhereN("Published", false)
	err := store.Get().Load(query, &items)
	return items, err
}

// MarkOutboxPublished 执行本文件能力对应的业务操作。
func MarkOutboxPublished(outbox *Outbox) error {
	return MarkOutboxPublishedWith(store.Get(), outbox)
}

// MarkOutboxPublishedWith 在指定事务中将 Outbox 事件标记为已发布。
func MarkOutboxPublishedWith(action persistencetypes.IDataAction, outbox *Outbox) error {
	outbox.Published = true
	outbox.SetUpdatedAt(time.Now().UTC())
	return action.Update(outbox)
}

// MarkOutboxPublishedByIDs 在当前事务中按主键批量标记事件已发布。
func MarkOutboxPublishedByIDs(action persistencetypes.IDataAction, ids []uint) error {
	if action == nil {
		return errors.New("数据操作器不能为空")
	}
	unique := uniqueOutboxIDs(ids)
	for _, id := range ids {
		if id == 0 {
			return errors.New("Outbox 主键不能为空")
		}
	}
	if len(unique) == 0 {
		return nil
	}
	var items []*Outbox
	query := store.NewSearch(NewOutbox(), len(unique))
	query.AddWhereNS("ID", persistencetypes.SymbolIn, unique)
	if err := action.Load(query, &items); err != nil {
		return err
	}
	// 先校验完整集合，再更新，缺失记录不能冒充幂等成功。
	found := make(map[uint]bool, len(items))
	for _, item := range items {
		if item != nil {
			found[item.ID] = true
		}
	}
	for _, id := range unique {
		if !found[id] {
			return errors.New("Outbox 事件不存在")
		}
	}
	for _, item := range items {
		if item == nil || item.Published {
			continue
		}
		if err := MarkOutboxPublishedWith(action, item); err != nil {
			return err
		}
	}
	return nil
}

func uniqueOutboxIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
