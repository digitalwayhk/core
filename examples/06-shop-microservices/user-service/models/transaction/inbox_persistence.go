package transaction

import (
	"sync"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/internal/store"
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
		if items[0].Processed {
			return nil
		}
		if err := operation(); err != nil {
			return err
		}
		items[0].Processed = true
		items[0].SetUpdatedAt(time.Now().UTC())
		return store.Get().Update(items[0])
	}
	item := NewInbox()
	item.TraceID = traceID
	item.EventID, item.EventType = eventID, eventType
	item.SetHashcode(item.GetHash())
	if err := store.Get().Insert(item); err != nil {
		return err
	}
	if err := operation(); err != nil {
		return err
	}
	item.Processed = true
	item.SetUpdatedAt(time.Now().UTC())
	return store.Get().Update(item)
}
