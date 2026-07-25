// 本文件定义当前服务交易事实、Outbox、Inbox 或投影模型能力。
package transaction

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/internal/store"
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
	outbox.Published = true
	outbox.SetUpdatedAt(time.Now().UTC())
	return store.Get().Update(outbox)
}
