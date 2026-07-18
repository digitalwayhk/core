package event

import (
	"context"
	"errors"
	"sync"
)

// Subscription 描述业务事件订阅。Subject 决定外部通道，EventType 是可选过滤条件；
// EventType 为空表示订阅该 Subject 下所有事件类型。
type Subscription struct {
	Subject   string
	EventType string
	Reliable  bool
	Handler   func(context.Context, *Envelope) error
}

type externalSubscriptionRef struct {
	cancel func()
	count  int
}

func subscriptionKey(reliable bool, subject string) string {
	if reliable {
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

func (b *ServiceEventBridge) subscribeExternalRef(ctx context.Context, reliable bool, subject string) (func(), error) {
	if subject == "" {
		return func() {}, nil
	}
	b.externalSubMu.Lock()
	if b.externalSubscriptions == nil {
		b.externalSubscriptions = make(map[string]*externalSubscriptionRef)
	}
	key := subscriptionKey(reliable, subject)
	if ref := b.externalSubscriptions[key]; ref != nil {
		ref.count++
		b.externalSubMu.Unlock()
		return func() {
			b.releaseExternalRef(key)
		}, nil
	}
	b.externalSubMu.Unlock()

	var cancel func()
	var err error
	if reliable {
		cancel, err = b.SubscribeExternalControl(ctx, subject)
	} else {
		cancel, err = b.SubscribeExternal(ctx, subject)
	}
	if err != nil {
		return nil, err
	}

	b.externalSubMu.Lock()
	if ref := b.externalSubscriptions[key]; ref != nil {
		ref.count++
		b.externalSubMu.Unlock()
		cancel()
		return func() { b.releaseExternalRef(key) }, nil
	}
	b.externalSubscriptions[key] = &externalSubscriptionRef{cancel: cancel, count: 1}
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
	if sub.Subject == "" && sub.EventType == "" {
		return errors.New("event subscription subject and event type cannot both be empty")
	}
	if sub.Handler == nil {
		return errors.New("event subscription handler is nil")
	}
	return nil
}
