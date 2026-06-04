package event

import (
	"context"
	"encoding/json"

	"github.com/digitalwayhk/core/pkg/server/mq"
)

// MQBridge connects an in-process Stream to an external MQ provider, enabling
// cross-service event delivery via message queues.
//
// Publish path:  event.Envelope → JSON → MQProvider.Publish
// Subscribe path: MQProvider message → JSON → event.Envelope → Stream.Publish
type MQBridge struct {
	stream  *Stream
	manager *mq.MQManager
}

// NewMQBridge creates a bridge between the given Stream and MQManager.
func NewMQBridge(stream *Stream, manager *mq.MQManager) *MQBridge {
	return &MQBridge{stream: stream, manager: manager}
}

// Publish serialises env to JSON and delivers it to the MQ provider on subject.
func (b *MQBridge) Publish(ctx context.Context, subject string, env *Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return b.manager.Publish(ctx, subject, data, nil)
}

// Subscribe registers an MQ subscription on subject. Each incoming MQ message
// is deserialised as an Envelope and published to the in-process Stream,
// triggering all registered Stream handlers for that event type.
// The returned cancel function stops the MQ subscription.
//
// Current semantics: ack is always called after the stream delivery attempt,
// regardless of whether any stream handler returns an error. This matches the
// fire-and-forget contract of Stream.Publish.
func (b *MQBridge) Subscribe(ctx context.Context, subject string) (cancel func(), err error) {
	return b.manager.Subscribe(ctx, subject, func(msg *mq.Message) {
		env := &Envelope{}
		if jsonErr := json.Unmarshal(msg.Data, env); jsonErr != nil {
			return
		}
		_ = b.stream.Publish(ctx, env)
		if msg.Ack != nil {
			_ = msg.Ack()
		}
	})
}
