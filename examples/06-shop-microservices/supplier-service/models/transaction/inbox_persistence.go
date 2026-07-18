// 本文件定义当前服务交易事实、Outbox、Inbox 或投影模型能力。
package transaction

import (
	"sync"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/internal/store"
)

var inboxMu sync.Mutex

// ProcessInbox 执行本文件能力对应的业务操作。
func ProcessInbox(traceID, eventID, eventType string, operation func() error) error {
	inboxMu.Lock()
	defer inboxMu.Unlock()
	if err := store.EnsureModel(NewInbox()); err != nil {
		return err
	}
	var items []*Inbox
	q := store.NewSearch(NewInbox(), 1)
	q.AddWhereN("EventID", eventID)
	if err := store.Get().Load(q, &items); err != nil {
		return err
	}
	if len(items) > 0 {
		return nil
	}
	if err := operation(); err != nil {
		return err
	}
	item := NewInbox()
	item.TraceID = traceID
	item.EventID, item.EventType = eventID, eventType
	item.SetHashcode(item.GetHash())
	return store.Get().Insert(item)
}
