// 本文件为应用提供同归属范围的多主题可靠批次，保留原Envelope与业务事务语义。
package event

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
)

// OwnedSubscription 显式要求持续归属，不等同于原SharedKeys单主题执行token。
type OwnedSubscription struct {
	Manifest   ownership.Manifest
	MaxOwned   int
	Key        func(*Envelope) (string, error)
	Activate   func(context.Context, ownership.Credential) error
	Restore    func(context.Context, ownership.Credential, ownership.ReferenceSnapshot) error
	Due        func(context.Context, ownership.Credential) (int64, error)
	NoDueTasks bool
	Handler    func(context.Context, ownership.Credential, []*Envelope) error
}

// OwnedExternalSubscriber 是外部Provider适配器的可选持续归属能力。
type OwnedExternalSubscriber interface {
	SubscribeOwned(context.Context, OwnedSubscription, string) (*ownership.Runtime, error)
}

// SubscribeOwned 使用逻辑服务原组；instanceID必须来自ServiceContext而非业务事件。
func (b *ServiceEventBridge) SubscribeOwned(ctx context.Context, sub OwnedSubscription, instanceID string) (*ownership.Runtime, error) {
	if b == nil || b.closed.Load() {
		return nil, ErrServiceEventBridgeClosed
	}
	if sub.Manifest.Group != b.subscriberID || sub.Manifest.Service != b.subscriberID {
		return nil, ownership.ErrUnknown
	}
	b.externalMu.RLock()
	external := b.external
	b.externalMu.RUnlock()
	subscriber, ok := external.(OwnedExternalSubscriber)
	if !ok {
		return nil, ownership.ErrUnsupported
	}
	return subscriber.SubscribeOwned(ctx, sub, instanceID)
}
