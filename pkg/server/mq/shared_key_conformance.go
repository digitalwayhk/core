package mq

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// VerifySharedKeyReliableDistribution 检查两个独立连接在同一 subject/group 的分键进展。
// 此 helper 不证明进程隔离；Provider 验收还必须运行真实双进程故障测试。
func VerifySharedKeyReliableDistribution(first, second SharedKeyReliableMQProvider) error {
	for _, p := range []SharedKeyReliableMQProvider{first, second} {
		if p == nil || !p.SharedKeyReliableInfo().Valid() || !p.OrderedReliableInfo().Valid() || !p.SupportsKeyedReliableConcurrency() {
			return ErrSharedKeyReliableUnsupported
		}
	}
	publisher, ok := first.(MQProvider)
	if !ok {
		return ErrSharedKeyReliableUnsupported
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hold := make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(hold) })
	a, b := make(chan struct{}, 1), make(chan struct{}, 1)
	opts := ReliableSubscribeOptions{Group: "conformance-shared", SharedKeys: true, KeyConcurrency: 1, Count: 4}
	stop, err := first.SubscribeReliable(ctx, "conformance.shared", opts, func(m *Message) error {
		if string(m.Data) == "a" {
			select {
			case a <- struct{}{}:
			default:
			}
			select {
			case <-hold:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return fmt.Errorf("mq shared conformance: first instance took independent work while saturated")
	})
	if err != nil {
		return err
	}
	defer stop()
	if err = publisher.Publish(ctx, "conformance.shared", []byte("a"), &PublishOptions{OrderingKey: "A"}); err != nil {
		return err
	}
	select {
	case <-a:
	case <-ctx.Done():
		return fmt.Errorf("mq shared conformance: first handler did not start")
	}
	stop2, err := second.SubscribeReliable(ctx, "conformance.shared", opts, func(m *Message) error {
		if string(m.Data) == "b" {
			select {
			case b <- struct{}{}:
			default:
			}
			return nil
		}
		return fmt.Errorf("mq shared conformance: same key overlapped active lease")
	})
	if err != nil {
		return err
	}
	defer stop2()
	if err = publisher.Publish(ctx, "conformance.shared", []byte("b"), &PublishOptions{OrderingKey: "B"}); err != nil {
		return err
	}
	select {
	case <-b:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("mq shared conformance: second connection made no progress while first key blocked")
	}
}
