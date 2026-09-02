package mq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// VerifyOrderedReliableFailureBarrier 运行最小 conformance：同 key 失败时后续不得越过。
// 用于拒绝「Info 合法但不阻断」的撒谎 provider（§7.10）。
func VerifyOrderedReliableFailureBarrier(provider OrderedReliableMQProvider) error {
	if provider == nil {
		return ErrOrderedReliableUnsupported
	}
	if !provider.OrderedReliableInfo().Valid() {
		return ErrOrderedReliableUnsupported
	}
	base, ok := provider.(MQProvider)
	if !ok {
		return fmt.Errorf("mq conformance: provider must implement MQProvider")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var (
		mu      sync.Mutex
		got     []string
		allowM3 atomic.Bool
		m3Fail  atomic.Int32
	)
	subCancel, err := provider.SubscribeReliable(ctx, "conformance.fills", ReliableSubscribeOptions{
		Group: "conformance", MinIdle: 100 * time.Millisecond, ClaimInterval: 50 * time.Millisecond,
	}, func(msg *Message) error {
		body := string(msg.Data)
		if body == "m3" && !allowM3.Load() {
			m3Fail.Add(1)
			return errors.New("conformance barrier")
		}
		mu.Lock()
		got = append(got, body)
		mu.Unlock()
		return nil
	})
	if err != nil {
		return err
	}
	defer subCancel()

	for _, body := range []string{"m1", "m2", "m3", "m4"} {
		if err := base.Publish(ctx, "conformance.fills", []byte(body), &PublishOptions{
			OrderingKey: "k", IdempotencyKey: body,
		}); err != nil {
			return err
		}
	}
	// 等待至少一次 m3 失败与 m1/m2 完成
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if m3Fail.Load() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if m3Fail.Load() < 1 {
		return fmt.Errorf("mq conformance: expected handler failure on m3")
	}
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	snapshot := append([]string(nil), got...)
	mu.Unlock()
	for _, body := range snapshot {
		if body == "m4" {
			return fmt.Errorf("mq conformance: m4 executed while m3 blocked (lying/no barrier)")
		}
	}
	for _, want := range []string{"m1", "m2"} {
		found := false
		for _, body := range snapshot {
			if body == want {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("mq conformance: missing %s before barrier, got %v", want, snapshot)
		}
	}
	return nil
}

// VerifyKeyedReliableConcurrency 证明显式 opt-in 后至少两个不同 OrderingKey
// 能同时进入 handler。它不替代失败屏障 conformance，发布门禁应同时运行两者。
func VerifyKeyedReliableConcurrency(provider KeyedReliableMQProvider) error {
	if provider == nil || !provider.SupportsKeyedReliableConcurrency() {
		return ErrKeyedReliableSubscribeUnsupported
	}
	base, ok := provider.(MQProvider)
	if !ok {
		return fmt.Errorf("mq keyed conformance: provider must implement MQProvider")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := make(chan string, 2)
	release := make(chan struct{})
	defer close(release)
	subCancel, err := provider.SubscribeReliable(ctx, "conformance.keyed", ReliableSubscribeOptions{
		Group: "conformance-keyed", MinIdle: 100 * time.Millisecond,
		ClaimInterval: 50 * time.Millisecond, KeyConcurrency: 2,
	}, func(msg *Message) error {
		started <- string(msg.Data)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		return err
	}
	defer subCancel()
	for _, item := range []struct{ body, key string }{{"a1", "a"}, {"b1", "b"}} {
		if err := base.Publish(ctx, "conformance.keyed", []byte(item.body), &PublishOptions{
			OrderingKey: item.key, IdempotencyKey: item.body,
		}); err != nil {
			return err
		}
	}
	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case body := <-started:
			got[body] = true
		case <-ctx.Done():
			return fmt.Errorf("mq keyed conformance: different keys did not overlap, got %v", got)
		}
	}
	if !got["a1"] || !got["b1"] {
		return fmt.Errorf("mq keyed conformance: unexpected messages %v", got)
	}
	return nil
}
