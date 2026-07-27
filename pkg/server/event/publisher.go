package event

import (
	"context"

	"github.com/digitalwayhk/core/pkg/server/observability"
)

// IPublisher is the single interface for publishing events.  Code that emits
// events should depend on IPublisher rather than on *Stream or *MQBridge
// directly, so the underlying transport (in-process vs. MQ) can be swapped
// without changing business logic.
type IPublisher interface {
	Publish(ctx context.Context, env *Envelope) error
}

// Publisher routes Publish calls to an MQBridge when one is available,
// falling back to the local in-process Stream otherwise.  This ensures
// single-node deployments work without any MQ configuration.
type Publisher struct {
	stream  *Stream
	bridge  *MQBridge
	subject string // MQ subject used when publishing via the bridge
	source  string // logical source service name for metrics
}

// NewPublisher returns a Publisher that uses bridge (if non-nil) for cross-node
// delivery, or stream for local in-process delivery.
// subject is the MQ topic/stream name used when bridge is active.
func NewPublisher(stream *Stream, bridge *MQBridge, subject string) *Publisher {
	return &Publisher{stream: stream, bridge: bridge, subject: subject}
}

// WithSourceService 设置发布方服务名（低基数指标标签）。
func (p *Publisher) WithSourceService(name string) *Publisher {
	if p != nil {
		p.source = name
	}
	return p
}

// Publish emits env.  If a bridge is configured the envelope is serialised and
// sent to the MQ provider; otherwise it is dispatched to in-process handlers.
func (p *Publisher) Publish(ctx context.Context, env *Envelope) error {
	eventType := ""
	if env != nil {
		eventType = env.Type
	}
	var err error
	if p.bridge != nil {
		err = p.bridge.Publish(ctx, p.subject, env)
	} else if p.stream != nil {
		err = p.stream.Publish(ctx, env)
	}
	result := observability.ResultSuccess
	if err != nil {
		result = observability.ClassifyError(err)
	}
	observability.RecordEventPublish(p.source, p.subject, eventType, result)
	return err
}
