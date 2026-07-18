package event

import "time"

type Metadata struct {
	EventID       string    `json:"eventID"`
	TraceID       string    `json:"traceID"`
	SchemaVersion int       `json:"schemaVersion"`
	EventType     string    `json:"eventType"`
	OccurredAt    time.Time `json:"occurredAt"`
	SourceService string    `json:"sourceService"`
	AggregateID   string    `json:"aggregateID"`
}
