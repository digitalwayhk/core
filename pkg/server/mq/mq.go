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

var ErrKeyedReliableSubscribeUnsupported = errors.New("mq: keyed reliable subscribe unsupported")

var ErrSharedKeyReliableUnsupported = errors.New("mq: shared key reliable subscribe unsupported")
var ErrReliableGroupModeConflict = errors.New("mq: reliable group mode or prefetch conflicts")

// ErrOrderedReliableUnsupported 表示当前 provider 未声明或不满足 ordered-reliable 契约。
var ErrOrderedReliableUnsupported = errors.New("mq: ordered reliable unsupported")

// ErrOrderingKeyRequired 表示已要求 ordered-reliable 时发布缺少 OrderingKey。
var ErrOrderingKeyRequired = errors.New("mq: ordering key required")

// Ordered-reliable capability 固定取值。
const (
	DeliveryAtLeastOnce       = "AT_LEAST_ONCE"
	OrderingScopeKey          = "ORDERING_KEY"
	AckAfterHandlerSuccess    = "AFTER_HANDLER_SUCCESS"
	FailurePolicyBlockSameKey = "BLOCK_SAME_KEY"
	FailoverPolicyKeepOrder   = "KEEP_KEY_ORDER"
)

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
	// OrderingKey 声明业务分区/顺序键；零值保持现有无序或非按 key 行为。
	OrderingKey string
}

// OrderedReliableCapability 描述 provider 的 ordered-reliable 行为声明。
// 仅声明不够，必须通过 conformance suite 证明实际行为。
type OrderedReliableCapability struct {
	Delivery       string
	OrderingScope  string
	AckPolicy      string
	FailurePolicy  string
	FailoverPolicy string
}

// DefaultOrderedReliableCapability 返回标准 at-least-once + 同 key 失败阻断声明。
func DefaultOrderedReliableCapability() OrderedReliableCapability {
	return OrderedReliableCapability{
		Delivery:       DeliveryAtLeastOnce,
		OrderingScope:  OrderingScopeKey,
		AckPolicy:      AckAfterHandlerSuccess,
		FailurePolicy:  FailurePolicyBlockSameKey,
		FailoverPolicy: FailoverPolicyKeepOrder,
	}
}

// Valid 检查能力声明字段是否完整且取值合法。
func (c OrderedReliableCapability) Valid() bool {
	return c.Delivery == DeliveryAtLeastOnce &&
		c.OrderingScope == OrderingScopeKey &&
		c.AckPolicy == AckAfterHandlerSuccess &&
		c.FailurePolicy == FailurePolicyBlockSameKey &&
		c.FailoverPolicy == FailoverPolicyKeepOrder
}

// ReliableSubscribeOptions 为可靠消费提供稳定的服务组和实例消费者身份。
type ReliableSubscribeOptions struct {
	Group         string
	Consumer      string
	MinIdle       time.Duration
	ClaimInterval time.Duration
	// Count 在 Redis 分 key 模式中同时限制分页和本 owner 的预取预算；
	// 有效值至少为 KeyConcurrency，未设置时为 KeyConcurrency*4。
	// 接管的既有超限 PEL 只分页恢复；串行模式仍固定为 1。
	Count int64
	// KeyConcurrency 是同一 active owner 内可并行处理的 OrderingKey 数。
	// 未启用 SharedKeys 时，零值和 1 保持整 subject 串行。
	KeyConcurrency int
	// SharedKeys 显式要求同 subject/group 的不同 key 由多个实例分担。
	// false 保留旧行为；Count 在该模式中是所有实例共享的 PEL 预算，必须一致。
	SharedKeys bool
	// 以下只由 Redis provider 为一次执行注入，不属于应用可设置的合同。
	ownerKey   string
	ownerToken string
	// lifecycle 仅由 MQManager 从已冻结的 Subject manifest 注入。
	// 应用必须通过 RequireMessageLifecycle 声明策略，不能绕过 Manager 设置。
	lifecycle *LifecyclePolicy
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

// KeyedReliableMQProvider 显式声明 provider 能正确实现同 key 串行、不同 key 并行。
// 仅接受 ReliableSubscribeOptions 字段但忽略它，不构成该能力。
type KeyedReliableMQProvider interface {
	ReliableMQProvider
	SupportsKeyedReliableConcurrency() bool
}

// OrderedReliableMQProvider 是可选扩展：在可靠 ACK 之上保证同 OrderingKey 有序与失败阻断。
// 启动检查可做类型断言与 Info 合法性校验；行为以 conformance suite 为准。
type OrderedReliableMQProvider interface {
	ReliableMQProvider
	OrderedReliableInfo() OrderedReliableCapability
}

// SharedKeyReliableCapability 将跨实例分担与单 owner 内的并发能力区分。
// 顺序仅限同 subject/group/key；不提供跨 subject 全局顺序或业务数据库 fencing。
type SharedKeyReliableCapability struct {
	Ordered         OrderedReliableCapability
	Scope           string
	FencedAck       bool
	BoundedPrefetch bool
}

const SharedKeyOrderingScope = "SUBJECT_GROUP_ORDERING_KEY"

func DefaultSharedKeyReliableCapability() SharedKeyReliableCapability {
	return SharedKeyReliableCapability{DefaultOrderedReliableCapability(), SharedKeyOrderingScope, true, true}
}

func (c SharedKeyReliableCapability) Valid() bool {
	return c.Ordered.Valid() && c.Scope == SharedKeyOrderingScope && c.FencedAck && c.BoundedPrefetch
}

// SharedKeyReliableMQProvider 是显式的跨实例分键分担能力，必须另做真实多进程验收。
type SharedKeyReliableMQProvider interface {
	OrderedReliableMQProvider
	KeyedReliableMQProvider
	SharedKeyReliableInfo() SharedKeyReliableCapability
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
