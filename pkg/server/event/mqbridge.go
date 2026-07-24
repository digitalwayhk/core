package event

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"

	"github.com/digitalwayhk/core/pkg/server/mq"
)

// ErrOrderingKeyRequired 表示已声明 ordered-reliable 时 Envelope.ShardKey 为空。
var ErrOrderingKeyRequired = errors.New("event: ordering key required")

// MQBridge connects an in-process Stream to an external MQ provider, enabling
// cross-service event delivery via message queues.
//
// Publish path:  event.Envelope → JSON → MQProvider.Publish
// Subscribe path: MQProvider message → JSON → event.Envelope → Stream.Publish
type MQBridge struct {
	stream                 *Stream
	manager                *mq.MQManager
	requireOrderedReliable atomic.Bool
}

// NewMQBridge creates a bridge between the given Stream and MQManager.
func NewMQBridge(stream *Stream, manager *mq.MQManager) *MQBridge {
	return &MQBridge{stream: stream, manager: manager}
}

// EnsureOrderedReliable 校验底层 provider 支持 ordered-reliable，并开启发布侧空 key 门禁。
func (b *MQBridge) EnsureOrderedReliable() error {
	if b == nil || b.manager == nil {
		return mq.ErrOrderedReliableUnsupported
	}
	if err := b.manager.RequireOrderedReliable(); err != nil {
		return err
	}
	b.requireOrderedReliable.Store(true)
	return nil
}

// RequiresOrderedReliable 报告是否已开启 ordered-reliable 发布门禁。
func (b *MQBridge) RequiresOrderedReliable() bool {
	return b != nil && b.requireOrderedReliable.Load()
}

// Publish serialises env to JSON and delivers it to the MQ provider on subject.
// IdempotencyKey 与 ShardKey 透传为 PublishOptions，供 provider 做 dedup 与分区。
func (b *MQBridge) Publish(ctx context.Context, subject string, env *Envelope) error {
	if env == nil {
		return ErrInvalidPublishRequest
	}
	if b.requireOrderedReliable.Load() && env.ShardKey == "" {
		return ErrOrderingKeyRequired
	}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	opts := &mq.PublishOptions{
		IdempotencyKey: env.IdempotencyKey,
		OrderingKey:    env.ShardKey,
	}
	return b.manager.Publish(ctx, subject, data, opts)
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

// SubscribeReliable 使用逻辑服务名作为 consumer group，只有全部控制 Handler
// 成功后 Provider 才确认消息。
func (b *MQBridge) SubscribeReliable(ctx context.Context, subject, subscriberID string) (func(), error) {
	return b.manager.SubscribeReliable(ctx, subject, mq.ReliableSubscribeOptions{Group: subscriberID}, func(msg *mq.Message) error {
		env := &Envelope{}
		if err := json.Unmarshal(msg.Data, env); err != nil {
			return err
		}
		return b.stream.PublishControl(ctx, env)
	})
}
