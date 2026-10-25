// 本文件用独占真实Redis和正式ServiceContext验证Outbox确认与消费ACK的独立边界。
package event_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type brokerOnlyService struct{}

func (*brokerOnlyService) ServiceName() string      { return "broker-only-fixture" }
func (*brokerOnlyService) Routers() []types.IRouter { return nil }
func brokerOnlyContext(t *testing.T, prefix string) *router.ServiceContext {
	return brokerOnlyContextAt(t, prefix, os.Getenv("CORE_TEST_REDIS_ADDR"))
}

func brokerOnlyContextAt(t *testing.T, prefix, addr string) *router.ServiceContext {
	t.Helper()
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR 专属Redis")
	}
	cfg := config.NewServiceDefaultConfig("broker-only-fixture", 29992)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "on"
	cfg.MQ.Provider = "redis-stream"
	cfg.MQ.RedisStream = config.RedisStreamMQConfig{Addr: addr, Prefix: prefix, CommandPoolSize: 4, BlockingReadPoolSize: 4}
	sc := router.NewServiceContextWithConfig(&brokerOnlyService{}, cfg)
	t.Cleanup(func() { sc.SetRunState(false); require.NoError(t, sc.ShutdownError()) })
	return sc
}

// TestBrokerOnlyRedisOutboxProgressBeforeHandlerCompletion 使用屏障而非延迟SLA，验证Broker已收、发布标记和后续独立事件均先完成。
func TestBrokerOnlyRedisOutboxProgressBeforeHandlerCompletion(t *testing.T) {
	prefix := fmt.Sprintf("core:broker-only:outbox:%d", time.Now().UnixNano())
	sc := brokerOnlyContext(t, prefix)
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: os.Getenv("CORE_TEST_REDIS_ADDR")})
	defer client.Close()
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	sub := brokerSubscription(t, "events", "projection", true, func(_ context.Context, e *event.Envelope) error {
		require.Equal(t, "broker-only-fixture", e.Source)
		entered <- struct{}{}
		<-release
		return nil
	})
	stop, err := sc.SubscribeEvent(sub)
	require.NoError(t, err)
	defer stop()
	store := &outboxStoreStub{items: []event.OutboxMessage{
		{ID: 1, EventID: "derived-1", EventType: "projection", Subject: "events", ShardKey: "A"},
		{ID: 2, EventID: "receipt-2", EventType: "receipt", Subject: "events", ShardKey: "B"},
	}}
	require.NoError(t, sc.UseOutbox(store))
	sc.NotifyOutbox()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("未进入派生投影handler")
	}
	require.Eventually(t, func() bool { store.mu.Lock(); defer store.mu.Unlock(); return len(store.marked) == 2 }, 2*time.Second, 10*time.Millisecond, "handler仍被屏障阻塞时Outbox应能确认并外发后续事件")
	key := prefix + ":events"
	require.EqualValues(t, 2, client.XLen(ctx, key).Val())
	pending, err := client.XPending(ctx, key, "broker-only-fixture").Result()
	require.NoError(t, err)
	require.GreaterOrEqual(t, pending.Count, int64(1), "消费完成前不能ACK")
	once.Do(func() { close(release) })
	require.Eventually(t, func() bool {
		value, err := client.XPending(ctx, key, "broker-only-fixture").Result()
		return err == nil && value.Count == 0
	}, 3*time.Second, 10*time.Millisecond)
}

// TestBrokerOnlyRedisPublishFailureRetainsOutbox 仅切断本测试代理，不停共享Redis，真实外发失败不确认Outbox，恢复后原身份收敛。
func TestBrokerOnlyRedisPublishFailureRetainsOutbox(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR")
	}
	proxy := newSharedRedisProxy(t, addr)
	prefix := fmt.Sprintf("core:broker-only:failure:%d", time.Now().UnixNano())
	sc := brokerOnlyContextAt(t, prefix, proxy.listener.Addr().String())
	calls := make(chan string, 2)
	stop, err := sc.SubscribeEvent(event.Subscription{Subject: "events", Reliable: true, BrokerOnly: true, Handler: func(_ context.Context, e *event.Envelope) error { calls <- e.ID; return nil }})
	require.NoError(t, err)
	defer stop()
	proxy.disconnect(true)
	store := &outboxStoreStub{items: []event.OutboxMessage{{ID: 1, EventID: "failure-retained", EventType: "projection", Subject: "events", ShardKey: "A"}}}
	require.NoError(t, sc.UseOutbox(store))
	sc.NotifyOutbox()
	require.Eventually(t, func() bool {
		return sc.ServiceEventBridge.OutboxRuntimeMetricProvider().RuntimeMetricSnapshot(context.Background()).Gauges["publish_fail"] > 0
	}, 8*time.Second, 10*time.Millisecond)
	store.mu.Lock()
	require.Empty(t, store.marked)
	require.Len(t, store.items, 1)
	store.mu.Unlock()
	select {
	case <-calls:
		t.Fatal("未进入Broker的事件不得本地消费")
	default:
	}
	proxy.disconnect(false)
	sc.NotifyOutbox()
	select {
	case id := <-calls:
		require.Equal(t, "failure-retained", id)
	case <-time.After(8 * time.Second):
		t.Fatal("连接恢复后原事件未消费")
	}
	require.Eventually(t, func() bool { store.mu.Lock(); defer store.mu.Unlock(); return len(store.marked) == 1 }, 3*time.Second, 10*time.Millisecond)
}

// TestBrokerOnlyRedisCancellationDoesNotAckInFlight 真实PEL验证取消和Close超时不遗弃在途handler、不把迟到nil当成功ACK。
func TestBrokerOnlyRedisCancellationDoesNotAckInFlight(t *testing.T) {
	prefix := fmt.Sprintf("core:broker-only:cancel:%d", time.Now().UnixNano())
	sc := brokerOnlyContext(t, prefix)
	bridge := sc.ServiceEventBridge
	client := redis.NewClient(&redis.Options{Addr: os.Getenv("CORE_TEST_REDIS_ADDR")})
	defer client.Close()
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	stop, err := sc.SubscribeEvent(event.Subscription{Subject: "events", Reliable: true, BrokerOnly: true, Handler: func(ctx context.Context, _ *event.Envelope) error {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release
		return nil
	}})
	require.NoError(t, err)
	e := brokerEnvelope("events", "changed")
	require.NoError(t, bridge.Publish(context.Background(), event.PublishRequest{Class: event.ControlDelivery, External: true, Subject: "events", Envelope: e}))
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler未开始")
	}
	stop()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, bridge.Close(ctx), context.DeadlineExceeded)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("取消未到handler")
	}
	pending, err := client.XPending(context.Background(), prefix+":events", "broker-only-fixture").Result()
	require.NoError(t, err)
	require.EqualValues(t, 1, pending.Count)
	once.Do(func() { close(release) })
	require.NoError(t, bridge.Close(context.Background()))
	pending, err = client.XPending(context.Background(), prefix+":events", "broker-only-fixture").Result()
	require.NoError(t, err)
	require.EqualValues(t, 1, pending.Count, "迟到nil不能确认原消息")
}
