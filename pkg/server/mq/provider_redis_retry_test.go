package mq_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newRetryRedisProvider(t *testing.T) (*mq.MQManager, *redis.Client, string) {
	t.Helper()
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT RUN: 设置 CORE_TEST_REDIS_ADDR 后运行 Redis retry/DLQ 真 Broker 测试")
	}
	prefix := fmt.Sprintf("core:test:retry:%d", time.Now().UnixNano())
	provider := mq.NewRedisStreamProvider(addr, prefix, 0)
	require.NoError(t, provider.Connect(context.Background()))
	manager := mq.NewManager()
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent(provider.Name()))
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() {
		keys, _ := client.Keys(context.Background(), prefix+"*").Result()
		if len(keys) > 0 {
			_ = client.Del(context.Background(), keys...).Err()
		}
		_ = client.Close()
		_ = manager.Close()
	})
	return manager, client, prefix
}

func redisRetryPolicy(subject, group, dlq string, max int, backoff time.Duration) mq.LifecyclePolicy {
	return mq.LifecyclePolicy{
		Subject:        subject,
		RequiredGroups: []mq.ConsumerGroupRequirement{{Name: group, Start: mq.StartFromAllRetained}},
		Retry: mq.RetryPolicy{
			MaxDeliveries: max, DeadLetterSubject: dlq, Backoff: []time.Duration{backoff},
		},
	}
}

func TestRedisBoundedRetryKeepsMessagePendingBeforeLimit(t *testing.T) {
	manager, client, prefix := newRetryRedisProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	policy := redisRetryPolicy("fills", "positions", "fills.dlq", 3, time.Hour)
	attempted := make(chan struct{}, 1)
	var attempts atomic.Int32
	require.NoError(t, manager.RequireMessageLifecycle(ctx, policy))
	cancelSub, err := manager.SubscribeReliable(ctx, policy.Subject, mq.ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-a", MinIdle: time.Second,
		ClaimInterval: 500 * time.Millisecond,
	}, func(*mq.Message) error {
		if attempts.Add(1) == 1 {
			attempted <- struct{}{}
		}
		return errors.New("temporary failure")
	})
	require.NoError(t, err)
	defer cancelSub()
	require.NoError(t, manager.Publish(ctx, policy.Subject, []byte("fill-1"), nil))
	select {
	case <-attempted:
	case <-ctx.Done():
		t.Fatal("handler was not invoked")
	}

	require.Eventually(t, func() bool {
		return client.XPending(ctx, prefix+":"+policy.Subject, "positions").Val().Count == 1
	}, time.Second, 10*time.Millisecond)
	require.Zero(t, client.XLen(ctx, prefix+":"+policy.Retry.DeadLetterSubject).Val())
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, int32(1), attempts.Load(), "retry backoff must prevent a hot retry loop")
}

func TestRedisBoundedRetryAtomicallyMovesMessageToDeadLetter(t *testing.T) {
	manager, client, prefix := newRetryRedisProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	policy := redisRetryPolicy("fills", "positions", "fills.dlq", 2, 20*time.Millisecond)
	require.NoError(t, manager.RequireMessageLifecycle(ctx, policy))
	cancelSub, err := manager.SubscribeReliable(ctx, policy.Subject, mq.ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-a", MinIdle: 30 * time.Millisecond,
		ClaimInterval: 20 * time.Millisecond,
	}, func(*mq.Message) error { return errors.New("poison") })
	require.NoError(t, err)
	defer cancelSub()
	require.NoError(t, manager.Publish(ctx, policy.Subject, []byte("fill-1"), nil))

	require.Eventually(t, func() bool {
		pending := client.XPending(ctx, prefix+":"+policy.Subject, "positions").Val().Count
		return pending == 0 && client.XLen(ctx, prefix+":"+policy.Retry.DeadLetterSubject).Val() == 1
	}, 3*time.Second, 20*time.Millisecond)
	items := client.XRange(ctx, prefix+":"+policy.Retry.DeadLetterSubject, "-", "+").Val()
	require.Len(t, items, 1)
	require.Equal(t, "fill-1", items[0].Values["data"])
	require.Equal(t, policy.Subject, items[0].Values["original_subject"])
	require.Equal(t, "positions", items[0].Values["consumer_group"])
}

func TestRedisDeadLetterWriteFailureLeavesOriginalPending(t *testing.T) {
	manager, client, prefix := newRetryRedisProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	policy := redisRetryPolicy("fills", "positions", "fills.dlq", 1, 20*time.Millisecond)
	require.NoError(t, client.Set(ctx, prefix+":"+policy.Retry.DeadLetterSubject, "wrong-type", 0).Err())
	require.NoError(t, manager.RequireMessageLifecycle(ctx, policy))
	cancelSub, err := manager.SubscribeReliable(ctx, policy.Subject, mq.ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-a", MinIdle: 30 * time.Millisecond,
		ClaimInterval: 20 * time.Millisecond,
	}, func(*mq.Message) error { return errors.New("poison") })
	require.NoError(t, err)
	defer cancelSub()
	require.NoError(t, manager.Publish(ctx, policy.Subject, []byte("fill-1"), nil))

	require.Eventually(t, func() bool {
		return client.XPending(ctx, prefix+":"+policy.Subject, "positions").Val().Count == 1
	}, 2*time.Second, 20*time.Millisecond)
}

func TestRedisRetryAttemptStateSurvivesConsumerRestart(t *testing.T) {
	manager, client, prefix := newRetryRedisProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	policy := redisRetryPolicy("fills", "positions", "fills.dlq", 2, 20*time.Millisecond)
	require.NoError(t, manager.RequireMessageLifecycle(ctx, policy))
	firstAttempt := make(chan struct{}, 1)
	firstCancel, err := manager.SubscribeReliable(ctx, policy.Subject, mq.ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-a", MinIdle: 100 * time.Millisecond,
		ClaimInterval: 50 * time.Millisecond,
	}, func(*mq.Message) error {
		firstAttempt <- struct{}{}
		return errors.New("first process failure")
	})
	require.NoError(t, err)
	require.NoError(t, manager.Publish(ctx, policy.Subject, []byte("fill-1"), nil))
	select {
	case <-firstAttempt:
	case <-ctx.Done():
		t.Fatal("first consumer did not receive message")
	}
	firstCancel()

	secondCancel, err := manager.SubscribeReliable(ctx, policy.Subject, mq.ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-b", MinIdle: 30 * time.Millisecond,
		ClaimInterval: 20 * time.Millisecond,
	}, func(*mq.Message) error { return errors.New("second process failure") })
	require.NoError(t, err)
	defer secondCancel()
	require.Eventually(t, func() bool {
		pending := client.XPending(ctx, prefix+":"+policy.Subject, "positions").Val().Count
		return pending == 0 && client.XLen(ctx, prefix+":"+policy.Retry.DeadLetterSubject).Val() == 1
	}, 4*time.Second, 20*time.Millisecond)
}

func TestRedisMaxAckPendingStopsReadingNewKeys(t *testing.T) {
	manager, _, _ := newRetryRedisProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	policy := redisRetryPolicy("fills", "positions", "fills.dlq", 3, time.Hour)
	policy.Retry.MaxAckPending = 1
	require.NoError(t, manager.RequireMessageLifecycle(ctx, policy))
	first := make(chan struct{}, 1)
	second := make(chan struct{}, 1)
	cancelSub, err := manager.SubscribeReliable(ctx, policy.Subject, mq.ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-a", MinIdle: 100 * time.Millisecond,
		ClaimInterval: 30 * time.Millisecond, KeyConcurrency: 2,
	}, func(message *mq.Message) error {
		if string(message.Data) == "a" {
			select {
			case first <- struct{}{}:
			default:
			}
			return errors.New("poison")
		}
		second <- struct{}{}
		return nil
	})
	require.NoError(t, err)
	defer cancelSub()
	require.NoError(t, manager.Publish(ctx, policy.Subject, []byte("a"), &mq.PublishOptions{OrderingKey: "a"}))
	require.NoError(t, manager.Publish(ctx, policy.Subject, []byte("b"), &mq.PublishOptions{OrderingKey: "b"}))
	select {
	case <-first:
	case <-ctx.Done():
		t.Fatal("first key was not delivered")
	}
	select {
	case <-second:
		t.Fatal("MaxAckPending=1 时不应继续读取第二个 key")
	case <-time.After(150 * time.Millisecond):
	}
}
