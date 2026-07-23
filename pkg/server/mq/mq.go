// Package mq provides a unified interface for message-queue based communication.
// Concrete providers (Redis Streams, NATS JetStream, etc.) implement MQProvider.
// MQManager tracks the active provider and MQSwitcher handles zero-downtime switching.
package mq

import (
	"context"
	"errors"
	"time"
)

// ErrNotConnected is returned when an operation is attempted on a disconnected provider.
var ErrNotConnected = errors.New("mq: provider not connected")

var ErrReliableSubscribeUnsupported = errors.New("mq: reliable subscribe unsupported")

// Message is a single message received from the queue.
type Message struct {
	ID      string
	Subject string
	Data    []byte
	// Ack acknowledges the message so it will not be redelivered.
	Ack func() error
}

// PublishOptions carries optional metadata for publishing.
type PublishOptions struct {
	// Subject overrides the default topic/subject.
	Subject string
	// IdempotencyKey deduplicates messages at the broker level when supported.
	IdempotencyKey string
}

// ReliableSubscribeOptions 为可靠消费提供稳定的服务组和实例消费者身份。
type ReliableSubscribeOptions struct {
	Group         string
	Consumer      string
	MinIdle       time.Duration
	ClaimInterval time.Duration
	Count         int64
}

// ReliableMQProvider 是 MQProvider 的可选可靠消费能力。
// handler 返回 nil 后 Provider 才能 ACK；返回错误时消息必须保持 pending。
type ReliableMQProvider interface {
	SubscribeReliable(
		ctx context.Context,
		subject string,
		options ReliableSubscribeOptions,
		handler func(msg *Message) error,
	) (cancel func(), err error)
}

// MQProvider is the interface that every message-queue backend must implement.
type MQProvider interface {
	// Name returns a stable identifier for the provider (e.g. "redis-stream").
	Name() string

	// Connect establishes the connection to the broker.
	Connect(ctx context.Context) error

	// Close releases all resources.
	Close() error

	// Publish sends data to the given subject.
	Publish(ctx context.Context, subject string, data []byte, opts *PublishOptions) error

	// Subscribe registers handler for messages on subject.
	// The returned cancel function unsubscribes.
	Subscribe(ctx context.Context, subject string, handler func(msg *Message)) (cancel func(), err error)

	// Health returns nil when the provider is reachable and functional.
	Health(ctx context.Context) error
}
