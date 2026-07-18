package transaction

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/internal/store"
)

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

func MarkOutboxPublished(outbox *Outbox) error {
	outbox.Published = true
	outbox.SetUpdatedAt(time.Now().UTC())
	return store.Get().Update(outbox)
}
