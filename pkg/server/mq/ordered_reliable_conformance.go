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
