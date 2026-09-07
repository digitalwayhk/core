package mq_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newReliableRedisProvider(t *testing.T) *mq.RedisStreamProvider {
	t.Helper()
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("设置 CORE_TEST_REDIS_ADDR 后运行 Redis Streams 可靠订阅测试")
	}
	provider := mq.NewRedisStreamProvider(addr, fmt.Sprintf("core:test:event:%d", time.Now().UnixNano()), 0)
	require.NoError(t, provider.Connect(context.Background()))
	t.Cleanup(func() { require.NoError(t, provider.Close()) })
	return provider
}

func newReliableRedisProviderWithPrefix(t *testing.T, prefix string) *mq.RedisStreamProvider {
	t.Helper()
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("设置 CORE_TEST_REDIS_ADDR 后运行 Redis Streams 可靠订阅测试")
	}
	provider := mq.NewRedisStreamProvider(addr, prefix, 0)
	require.NoError(t, provider.Connect(context.Background()))
	t.Cleanup(func() { require.NoError(t, provider.Close()) })
	return provider
}

func TestRedisReliableSubscribersUseIndependentServiceGroups(t *testing.T) {
	provider := newReliableRedisProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	users := make(chan string, 1)
	suppliers := make(chan string, 1)
	options := func(group string) mq.ReliableSubscribeOptions {
		return mq.ReliableSubscribeOptions{Group: group, MinIdle: 100 * time.Millisecond, ClaimInterval: 50 * time.Millisecond}
	}
	cancelUsers, err := provider.SubscribeReliable(ctx, "order.changed", options("user-service"), func(message *mq.Message) error {
		users <- string(message.Data)
		return nil
	})
	require.NoError(t, err)
	defer cancelUsers()
	cancelSuppliers, err := provider.SubscribeReliable(ctx, "order.changed", options("supplier-service"), func(message *mq.Message) error {
		suppliers <- string(message.Data)
		return nil
	})
	require.NoError(t, err)
	defer cancelSuppliers()

	require.NoError(t, provider.Publish(ctx, "order.changed", []byte("created"), nil))
	require.Equal(t, "created", <-users)
	require.Equal(t, "created", <-suppliers)
}

func TestRedisReliableSubscriptionReclaimsFailedPendingMessage(t *testing.T) {
	provider := newReliableRedisProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	failed := make(chan struct{}, 1)
	firstCancel, err := provider.SubscribeReliable(ctx, "order.changed", mq.ReliableSubscribeOptions{
		Group: "user-service", Consumer: "user-a", MinIdle: 150 * time.Millisecond, ClaimInterval: 50 * time.Millisecond,
	}, func(*mq.Message) error {
		failed <- struct{}{}
		return errors.New("temporary inbox failure")
	})
	require.NoError(t, err)
	require.NoError(t, provider.Publish(ctx, "order.changed", []byte("created"), nil))
	<-failed
	firstCancel()

	reclaimed := make(chan string, 1)
	secondCancel, err := provider.SubscribeReliable(ctx, "order.changed", mq.ReliableSubscribeOptions{
		Group: "user-service", Consumer: "user-b", MinIdle: 150 * time.Millisecond, ClaimInterval: 50 * time.Millisecond,
	}, func(message *mq.Message) error {
		reclaimed <- string(message.Data)
		return nil
	})
	require.NoError(t, err)
	defer secondCancel()

	select {
	case value := <-reclaimed:
		require.Equal(t, "created", value)
	case <-ctx.Done():
		t.Fatal("失败的 Redis pending 消息未被重新认领")
	}
}

// TestRedisReliableTakeoverPreservesOrderAcrossMultiplePending 锁定：
// loader 将 01..10 读入 PEL 后不 ACK（多 pending）；B 接管后必须先排空 pending 再处理 11..20。
func TestRedisReliableTakeoverPreservesOrderAcrossMultiplePending(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("设置 CORE_TEST_REDIS_ADDR 后运行 Redis Streams 可靠订阅测试")
	}
	prefix := fmt.Sprintf("core:test:takeover:%d", time.Now().UnixNano())
	provider := newReliableRedisProviderWithPrefix(t, prefix)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const total = 20
	const pendingN = 10
	group := "positions-takeover"
	subject := "trade.fills"
	streamKey := prefix + ":" + subject
	loader := "loader-crash"

	// 用原始 Redis 把 01..10 读进 loader 的 PEL（不 ACK），构造真正多 pending。
	raw := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, raw.XGroupCreateMkStream(ctx, streamKey, group, "0").Err())
	for i := 1; i <= pendingN; i++ {
		body := fmt.Sprintf("%02d", i)
		require.NoError(t, raw.XAdd(ctx, &redis.XAddArgs{
			Stream: streamKey,
			Values: map[string]interface{}{
				"data": body, "ordering_key": "market-a", "idempotency_key": body,
			},
		}).Err())
	}
	entries, err := raw.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: group, Consumer: loader, Streams: []string{streamKey, ">"}, Count: int64(pendingN), Block: time.Second,
	}).Result()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Len(t, entries[0].Messages, pendingN)

	// 再发 11..20 作为「新消息」；B 必须先 reclaim 01..10 再读新。
	for i := pendingN + 1; i <= total; i++ {
		body := fmt.Sprintf("%02d", i)
		require.NoError(t, provider.Publish(ctx, subject, []byte(body), &mq.PublishOptions{
			OrderingKey: "market-a", IdempotencyKey: body,
		}))
	}

	got := make([]string, 0, total)
	done := make(chan struct{})
	secondCancel, err := provider.SubscribeReliable(ctx, subject, mq.ReliableSubscribeOptions{
		Group: group, Consumer: "owner-b", MinIdle: 30 * time.Millisecond, ClaimInterval: 20 * time.Millisecond,
	}, func(message *mq.Message) error {
		got = append(got, string(message.Data))
		if len(got) >= total {
			select {
			case <-done:
			default:
				close(done)
			}
		}
		return nil
	})
	require.NoError(t, err)
	defer secondCancel()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("timeout got=%v (len=%d)", got, len(got))
	}

	require.Equal(t, total, len(got), "got=%v", got)
	for i := 0; i < total; i++ {
		require.Equal(t, fmt.Sprintf("%02d", i+1), got[i], "full=%v", got)
	}
}

func TestRedisReliableKeyedConcurrencyRunsDifferentKeysInParallel(t *testing.T) {
	provider := newReliableRedisProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	started := make(chan string, 4)
	release := make(chan struct{})
	var (
		mu    sync.Mutex
		order []string
	)
	cancelSub, err := provider.SubscribeReliable(ctx, "fills", mq.ReliableSubscribeOptions{
		Group: "positions", Consumer: "owner-a", MinIdle: 100 * time.Millisecond,
		ClaimInterval: 50 * time.Millisecond, KeyConcurrency: 2,
	}, func(message *mq.Message) error {
		body := string(message.Data)
		started <- body
		<-release
		mu.Lock()
		order = append(order, body)
		mu.Unlock()
		return nil
	})
	require.NoError(t, err)
	defer cancelSub()

	for _, item := range []struct{ body, key string }{{"a1", "a"}, {"a2", "a"}, {"b1", "b"}} {
		require.NoError(t, provider.Publish(ctx, "fills", []byte(item.body), &mq.PublishOptions{
			OrderingKey: item.key, IdempotencyKey: item.body,
		}))
	}
	first := map[string]bool{}
	for len(first) < 2 {
		select {
		case body := <-started:
			first[body] = true
		case <-ctx.Done():
			t.Fatalf("different Redis keys did not overlap: %v", first)
		}
	}
	require.True(t, first["a1"])
	require.True(t, first["b1"])
	require.False(t, first["a2"])
	snapshot := provider.RuntimeMetricSnapshot(context.Background())
	require.Equal(t, "ok", snapshot.State)
	require.Equal(t, float64(2), snapshot.Gauges["key_concurrency"])
	require.Equal(t, float64(2), snapshot.Gauges["handler_inflight"])
	close(release)
	select {
	case body := <-started:
		require.Equal(t, "a2", body)
	case <-ctx.Done():
		t.Fatal("same-key successor did not resume")
	}
	require.Eventually(t, func() bool {
		return provider.RuntimeMetricSnapshot(context.Background()).Gauges["handler_peak"] >= 2
	}, time.Second, 10*time.Millisecond)
}

// TestRedisReliableKeyedPoisonKeyDoesNotBlockOtherKeys 验证失败 key 不阻塞其他 key，且恢复后同 key 不越序。
func TestRedisReliableKeyedPoisonKeyDoesNotBlockOtherKeys(t *testing.T) {
	provider := newReliableRedisProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	allowA := atomic.Bool{}
	aAttempted := make(chan struct{}, 1)
	bDone := make(chan struct{}, 1)
	aDone := make(chan string, 2)
	cancelSub, err := provider.SubscribeReliable(ctx, "fills", mq.ReliableSubscribeOptions{
		Group: "positions", Consumer: "owner-a", MinIdle: 100 * time.Millisecond,
		ClaimInterval: 50 * time.Millisecond, KeyConcurrency: 2,
	}, func(message *mq.Message) error {
		body := string(message.Data)
		if body == "a1" && !allowA.Load() {
			select {
			case aAttempted <- struct{}{}:
			default:
			}
			return errors.New("poison")
		}
		if body == "b1" {
			bDone <- struct{}{}
		} else {
			aDone <- body
		}
		return nil
	})
	require.NoError(t, err)
	defer cancelSub()

	for _, item := range []struct{ body, key string }{{"a1", "a"}, {"a2", "a"}, {"b1", "b"}} {
		require.NoError(t, provider.Publish(ctx, "fills", []byte(item.body), &mq.PublishOptions{
			OrderingKey: item.key, IdempotencyKey: item.body,
		}))
	}
	select {
	case <-bDone:
	case <-ctx.Done():
		t.Fatal("poison key blocked healthy key")
	}
	// 不同 key 并发执行，B 完成不代表 A 已被调度；分别等待两个事实。
	select {
	case <-aAttempted:
	case <-ctx.Done():
		t.Fatal("poison key was not attempted")
	}
	select {
	case body := <-aDone:
		t.Fatalf("same-key successor overtook poison message: %s", body)
	case <-time.After(100 * time.Millisecond):
	}

	allowA.Store(true)
	select {
	case body := <-aDone:
		require.Equal(t, "a1", body)
	case <-ctx.Done():
		t.Fatal("poison message did not recover")
	}
	select {
	case body := <-aDone:
		require.Equal(t, "a2", body)
	case <-ctx.Done():
		t.Fatal("same-key successor did not resume after recovery")
	}
}

func TestRedisReliableKeyedConformance(t *testing.T) {
	provider := newReliableRedisProvider(t)
	require.NoError(t, mq.VerifyKeyedReliableConcurrency(provider))
}
