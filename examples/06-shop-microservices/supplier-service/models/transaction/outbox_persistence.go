package transaction

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/internal/store"
)

func NewProductOutbox(traceID, eventID, eventType, subject string, payload interface{}) (*Outbox, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	item := NewOutbox()
	item.TraceID = strings.TrimSpace(traceID)
	item.EventID, item.EventType, item.Subject, item.Payload = strings.TrimSpace(eventID), eventType, subject, data
	item.SetHashcode(item.GetHash())
	return item, nil
}

func PendingOutbox() ([]*Outbox, error) {
	if err := store.EnsureModel(NewOutbox()); err != nil {
		return nil, err
	}
	var items []*Outbox
	q := store.NewSearch(NewOutbox(), 100)
	q.AddWhereN("Published", false)
	q.AddSortN("ID", true)
	err := store.Get().Load(q, &items)
	return items, err
}

func MarkOutboxPublished(item *Outbox) error {
	item.Published = true
	item.SetUpdatedAt(time.Now().UTC())
	return store.Get().Update(item)
}
