// 本文件通过真实ServiceContext/Redis检验特定topic发布失败、持续健康流与原身份恢复；只用独占测试Redis。
package event_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestOutboxRetryRedisWrongTypeWhileHealthy用Redis真实WRONGTYPE作为初始发布故障；恢复只改名合成故障字符串，不删除Stream/PEL或重置游标。
func TestOutboxRetryRedisWrongTypeWhileHealthy(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: 独占CORE_TEST_REDIS_ADDR")
	}
	ctx := context.Background()
	prefix := fmt.Sprintf("core:outbox-retry:%d", time.Now().UnixNano())
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	failedKey := prefix + ":failed"
	require.NoError(t, client.Set(ctx, failedKey, "Core合成故障：从未保存消息的错误类型", 0).Err())
	sc := brokerOnlyContext(t, prefix)
	var healthy atomic.Int64
	stop, err := sc.SubscribeEvent(event.Subscription{Subject: "healthy", Reliable: true, BrokerOnly: true, Handler: func(context.Context, *event.Envelope) error { healthy.Add(1); return nil }})
	require.NoError(t, err)
	defer stop()
	s := newRetryTrafficStore()
	store := retrySkipBatchStore{&retryBatchStore{store: retryPlainStore{s}, s: s}}
	t.Cleanup(func() { s.producing.Store(false) })
	require.NoError(t, sc.UseOutboxWithOptions(store, router.OutboxRuntimeOptions{KeyConcurrency: 8, BatchSize: 16}))
	sc.NotifyOutbox()
	require.Eventually(t, func() bool {
		metrics := sc.ServiceEventBridge.OutboxRuntimeMetricProvider().RuntimeMetricSnapshot(ctx)
		return healthy.Load() >= 32 && metrics.Gauges["publish_fail"] > 0
	}, 5*time.Second, 5*time.Millisecond, "必须同时观察真实Broker发布失败和独立健康消费")
	require.Equal(t, "string", client.Type(ctx, failedKey).Val())
	require.False(t, retryConfirmedPair(s))
	require.NoError(t, client.Rename(ctx, failedKey, prefix+":fault-original-preserved").Err())
	var mu sync.Mutex
	var ids []string
	stopFailed, err := sc.SubscribeEvent(event.Subscription{Subject: "failed", Reliable: true, BrokerOnly: true, KeyConcurrency: 2, Handler: func(_ context.Context, e *event.Envelope) error {
		mu.Lock()
		ids = append(ids, e.ID)
		mu.Unlock()
		return nil
	}})
	require.NoError(t, err)
	defer stopFailed()
	require.Eventually(t, func() bool {
		mu.Lock()
		n := len(ids)
		mu.Unlock()
		return retryConfirmedPair(s) && n >= 2
	}, 5*time.Second, 5*time.Millisecond, "健康生产持续时，修复Broker故障后原前序必须重新发布")
	require.True(t, s.producing.Load())
	mu.Lock()
	got := append([]string(nil), ids...)
	mu.Unlock()
	require.Equal(t, []string{"a1", "a2"}, got)
	require.EqualValues(t, 2, client.XLen(ctx, failedKey).Val())
	require.Equal(t, "string", client.Type(ctx, prefix+":fault-original-preserved").Val())
	require.Eventually(t, func() bool {
		v, e := client.XPending(ctx, failedKey, "broker-only-fixture").Result()
		return e == nil && v.Count == 0
	}, 3*time.Second, 5*time.Millisecond)
	t.Logf("真实Redis：healthy=%d full_batches=%d A原ID=%v streamA=2 PEL=0 健康生产仍开启", healthy.Load(), s.loads.Load(), got)
}
