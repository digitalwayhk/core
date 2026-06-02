// Package event provides CloudEvents-compatible event envelopes and routing
// for the core framework. Events carry trace, idempotency, and shard metadata
// in addition to the standard CloudEvents fields.
package event

import (
	"time"

	"github.com/google/uuid"
)

// Envelope wraps an event payload with CloudEvents-compatible metadata plus
// core-specific routing fields.
type Envelope struct {
	// CloudEvents standard fields (https://cloudevents.io)
	ID              string    `json:"id"`
	Source          string    `json:"source"`
	SpecVersion     string    `json:"specversion"`
	Type            string    `json:"type"`
	Subject         string    `json:"subject,omitempty"`
	Time            time.Time `json:"time"`
	DataContentType string    `json:"datacontenttype,omitempty"`
	Data            []byte    `json:"data,omitempty"`

	// core-specific routing metadata
	TraceID        string `json:"traceid,omitempty"`
	IdempotencyKey string `json:"idempotencykey,omitempty"`
	ShardKey       string `json:"shardkey,omitempty"`
}

// NewEnvelope creates an Envelope with a generated UUID, SpecVersion="1.0",
// DataContentType="application/json", and Time pre-populated.
func NewEnvelope(source, eventType string, data []byte) *Envelope {
	env := &Envelope{
		ID:          uuid.New().String(),
		Source:      source,
		SpecVersion: "1.0",
		Type:        eventType,
		Time:        time.Now().UTC(),
		Data:        data,
	}
	if len(data) > 0 {
		env.DataContentType = "application/json"
	}
	return env
}
