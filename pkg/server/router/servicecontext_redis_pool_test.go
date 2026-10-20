package router_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestServiceContextRealRedisPoolIsolation(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR")
	}
	name := fmt.Sprintf("pool-service-%d", time.Now().UnixNano())
	prefix := "core:" + name
	cfg := config.NewServiceDefaultConfig(name, 29990)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "on"
	cfg.MQ.Provider = "redis-stream"
	cfg.MQ.RedisStream = config.RedisStreamMQConfig{Addr: addr, Prefix: prefix, CommandPoolSize: 2, BlockingReadPoolSize: 4}
	sc := router.NewServiceContextWithConfig(&fakeService{name}, cfg)
	t.Cleanup(func() { sc.SetRunState(false); require.NoError(t, sc.ShutdownError()) })
	require.NotNil(t, sc.EventBridge)
	require.NoError(t, sc.RequireOrderedReliableByShardKey())
	for i := 0; i < 8; i++ {
		stop, err := sc.SubscribeEvent(event.Subscription{Subject: fmt.Sprintf("idle-%d", i), Reliable: true, Handler: func(context.Context, *event.Envelope) error { return nil }})
		require.NoError(t, err)
		defer stop()
	}
	received := make(chan int, 20)
	var failed atomic.Bool
	stop, err := sc.SubscribeEvent(event.Subscription{Subject: "active", Reliable: true, KeyConcurrency: 2, Handler: func(_ context.Context, e *event.Envelope) error {
		n, err := strconv.Atoi(string(e.Data))
		if err != nil {
			return err
		}
		if n == 0 && !failed.Swap(true) {
			return fmt.Errorf("fixture first delivery fails")
		}
		received <- n
		return nil
	}})
	require.NoError(t, err)
	defer stop()
	for i := 0; i < 20; i++ {
		env := event.NewEnvelope("fixture", "changed", []byte(strconv.Itoa(i)))
		env.Subject = "active"
		env.ShardKey = "A"
		require.NoError(t, sc.EventBridge.Publish(context.Background(), "active", env))
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for want := 0; want < 20; want++ {
		select {
		case got := <-received:
			require.Equal(t, want, got)
		case <-deadline.C:
			t.Fatal("正式ServiceContext→EventBridge→Provider链未推进")
		}
	}
	admin := redis.NewClient(&redis.Options{Addr: addr, PoolSize: 1})
	defer admin.Close()
	require.Eventually(t, func() bool { return admin.XPending(context.Background(), prefix+":active", name).Val().Count == 0 }, time.Second, time.Millisecond)
	snapshot := sc.MQManager.RuntimeMetricSnapshot(context.Background())
	require.EqualValues(t, 2, snapshot.Gauges["redis_command_pool_limit"])
	require.EqualValues(t, 4, snapshot.Gauges["redis_blocking_pool_limit"])
	require.EqualValues(t, 8, snapshot.Gauges["redis_blocking_subscriptions"])
}
