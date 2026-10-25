package event

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// Subscription 描述业务事件订阅。Subject 决定外部通道，EventType 是可选过滤条件；
// EventType 为空表示订阅该 Subject 下所有事件类型。
type Subscription struct {
	Subject   string
	EventType string
	Reliable  bool
	// BrokerOnly 仅允许外部可靠MQ入站执行；零值保留发布阶段的本地同步交付。
	BrokerOnly bool
	// Broadcast 让可靠控制事件的每个服务副本各自消费一次。
	// 只适用于低频、幂等的副本收敛事件，不得用于价格/成交数据面。
	Broadcast      bool
	KeyConcurrency int
	// SharedKeys 要求跨实例分键分担，不与 Broadcast 合用；零值兼容旧行为。
	SharedKeys bool
	// Prefetch 是共享模式的组级 PEL 预算，零值为归一化 KeyConcurrency*4。
	Prefetch int64
	Handler  func(context.Context, *Envelope) error
}

type externalSubscriptionRef struct {
	cancel         func()
	count          int
	keyConcurrency int
	sharedKeys     bool
	prefetch       int64
}

func subscriptionKey(reliable, broadcast bool, subject string) string {
	if reliable {
		if broadcast {
			return "broadcast-control:" + subject
		}
		return "control:" + subject
	}
	return "observer:" + subject
}

func combineCancels(cancels ...func()) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			for index := len(cancels) - 1; index >= 0; index-- {
				if cancels[index] != nil {
					cancels[index]()
				}
			}
		})
	}
}

func (b *ServiceEventBridge) subscribeExternalRef(
	ctx context.Context,
	reliable bool,
	broadcast bool,
	subject string,
	keyConcurrency int,
	sharedKeys bool,
	prefetch int64,
) (func(), error) {
	if subject == "" {
		return func() {}, nil
	}
	keyConcurrency = normalizeKeyConcurrency(keyConcurrency)
	if sharedKeys {
		prefetch = normalizeSharedPrefetch(prefetch, keyConcurrency)
	}
	b.externalSubMu.Lock()
	if b.closed.Load() {
		b.externalSubMu.Unlock()
		return nil, ErrServiceEventBridgeClosed
	}
	if b.externalSubscriptions == nil {
		b.externalSubscriptions = make(map[string]*externalSubscriptionRef)
	}
	key := subscriptionKey(reliable, broadcast, subject)
	if ref := b.externalSubscriptions[key]; ref != nil {
		if ref.keyConcurrency != keyConcurrency || ref.sharedKeys != sharedKeys || ref.prefetch != prefetch {
			b.externalSubMu.Unlock()
			return nil, errors.New("event subscription key concurrency conflicts for subject")
		}
		ref.count++
		b.externalSubMu.Unlock()
		return func() {
			b.releaseExternalRef(key)
		}, nil
	}

	var cancel func()
	var err error
	if reliable {
		subscriberID := b.subscriberID
		if broadcast {
			subscriberID = b.instanceSubscriberID
			if subscriberID == "" {
				b.externalSubMu.Unlock()
				return nil, errors.New("event reliable broadcast requires stable instance subscriber id")
			}
		}
		cancel, err = b.subscribeExternalControlWithOptions(ctx, subject, ReliableExternalSubscribeOptions{
			KeyConcurrency: keyConcurrency, SharedKeys: sharedKeys, Prefetch: prefetch,
		}, subscriberID)
	} else {
		cancel, err = b.SubscribeExternal(ctx, subject)
	}
	if err != nil {
		b.externalSubMu.Unlock()
		return nil, err
	}
	if cancel == nil {
		b.externalSubMu.Unlock()
		return nil, ErrExternalProviderUnavailable
	}
	b.externalSubscriptions[key] = &externalSubscriptionRef{
		cancel: cancel, count: 1, keyConcurrency: keyConcurrency, sharedKeys: sharedKeys, prefetch: prefetch,
	}
	b.externalSubMu.Unlock()
	return func() { b.releaseExternalRef(key) }, nil
}

func (b *ServiceEventBridge) releaseExternalRef(key string) {
	b.externalSubMu.Lock()
	ref := b.externalSubscriptions[key]
	if ref == nil {
		b.externalSubMu.Unlock()
		return
	}
	ref.count--
	if ref.count > 0 {
		b.externalSubMu.Unlock()
		return
	}
	delete(b.externalSubscriptions, key)
	b.externalSubMu.Unlock()
	if ref.cancel != nil {
		ref.cancel()
	}
}

func (b *ServiceEventBridge) closeExternalSubscriptions() {
	b.externalSubMu.Lock()
	refs := make([]*externalSubscriptionRef, 0, len(b.externalSubscriptions))
	for key, ref := range b.externalSubscriptions {
		refs = append(refs, ref)
		delete(b.externalSubscriptions, key)
	}
	b.externalSubMu.Unlock()
	for _, ref := range refs {
		if ref != nil && ref.cancel != nil {
			ref.cancel()
		}
	}
}

func validateSubscription(sub Subscription) error {
	if sub.BrokerOnly && (!sub.Reliable || strings.TrimSpace(sub.Subject) == "" || sub.Subject != strings.TrimSpace(sub.Subject)) {
		return errors.New("event broker-only requires reliable delivery and a valid subject")
	}
	if sub.Subject == "" && sub.EventType == "" {
		return errors.New("event subscription subject and event type cannot both be empty")
	}
	if sub.Handler == nil {
		return errors.New("event subscription handler is nil")
	}
	if sub.Prefetch < 0 || (!sub.SharedKeys && sub.Prefetch != 0) {
		return errors.New("event prefetch requires shared keys and cannot be negative")
	}
	if sub.KeyConcurrency < 0 {
		return errors.New("event subscription key concurrency cannot be negative")
	}
	if !sub.Reliable && sub.KeyConcurrency > 1 {
		return errors.New("event subscription key concurrency requires reliable delivery")
	}
	if sub.SharedKeys && (!sub.Reliable || sub.Subject == "" || sub.Broadcast) {
		return errors.New("event shared key subscription requires reliable subject and cannot broadcast")
	}
	if sub.Broadcast && !sub.Reliable {
		return errors.New("event broadcast subscription requires reliable delivery")
	}
	return nil
}

func normalizeKeyConcurrency(value int) int {
	if value <= 1 {
		return 1
	}
	return value
}

func normalizeSharedPrefetch(value int64, concurrency int) int64 {
	if value == 0 {
		return int64(concurrency) * 4
	}
	if value < int64(concurrency) {
		return int64(concurrency)
	}
	return value
}
