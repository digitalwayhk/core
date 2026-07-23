package mq_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/mq"
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
