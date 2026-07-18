package transaction

import (
	"sync"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/internal/store"
)

var inboxMu sync.Mutex

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
