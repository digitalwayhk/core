package event_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestMQBridgeRedisPoolDisconnectPendingRecovery(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR")
	}
	for _, n := range []int{1, 2} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			proxy := newSharedRedisProxy(t, addr)
			prefix := fmt.Sprintf("core:pool-network:%d", time.Now().UnixNano())
			provider := mq.NewRedisStreamProviderWithConfig(config.RedisStreamMQConfig{Addr: proxy.listener.Addr().String(), Prefix: prefix, CommandPoolSize: 2, BlockingReadPoolSize: 3})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			require.NoError(t, provider.Connect(ctx))
			manager := mq.NewManager()
			manager.Register(provider)
			require.NoError(t, manager.SetCurrent(provider.Name()))
			t.Cleanup(func() { require.NoError(t, manager.Close()) })
			stream := event.NewStream()
			bridge := event.NewMQBridge(stream, manager)
			require.NoError(t, bridge.EnsureOrderedReliable())
			admin := redis.NewClient(&redis.Options{Addr: addr, PoolSize: 1})
			defer admin.Close()
			publish := func(body string) {
				env := event.NewEnvelope("fixture", "changed", []byte(body))
				env.Subject = "active"
				env.ShardKey = "A"
				data, err := json.Marshal(env)
				require.NoError(t, err)
				require.NoError(t, admin.XAdd(ctx, &redis.XAddArgs{Stream: prefix + ":active", Values: map[string]any{"data": data, "ordering_key": "A"}}).Err())
			}
			publish("first")
			require.NoError(t, admin.XGroupCreate(ctx, prefix+":active", "group", "0").Err())
			_, err := admin.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "group", Consumer: "crashed", Streams: []string{prefix + ":active", ">"}, Count: 1, Block: -1}).Result()
			require.NoError(t, err)
			entered, hold := make(chan struct{}), make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(hold) }) }
			defer release()
			var attempts atomic.Int32
			success := make(chan string, 2)
			stopHandler, err := stream.SubscribeControl("changed", func(e *event.Envelope) error {
				if string(e.Data) == "first" && attempts.Add(1) == 1 {
					close(entered)
					<-hold
					return fmt.Errorf("fixture interrupted delivery")
				}
				success <- string(e.Data)
				return nil
			})
			require.NoError(t, err)
			defer stopHandler()
			stop, err := bridge.SubscribeReliableWithOptions(ctx, "active", "group", event.ReliableExternalSubscribeOptions{KeyConcurrency: n})
			require.NoError(t, err)
			defer stop()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("旧pending未接管")
			}
			proxy.disconnect(true)
			publish("second")
			release()
			failedCtx, stopProbe := context.WithTimeout(ctx, 500*time.Millisecond)
			env := event.NewEnvelope("fixture", "changed", []byte("probe"))
			env.ShardKey = "A"
			require.Error(t, bridge.Publish(failedCtx, "probe", env))
			stopProbe()
			require.EqualValues(t, 1, admin.XPending(ctx, prefix+":active", "group").Val().Count, "失败和断连不能ACK旧pending")
			proxy.disconnect(false)
			for _, want := range []string{"first", "second"} {
				select {
				case got := <-success:
					require.Equal(t, want, got)
				case <-ctx.Done():
					t.Fatal("连接恢复后pending/新消息未依序恢复")
				}
			}
			require.Eventually(t, func() bool { return admin.XPending(ctx, prefix+":active", "group").Val().Count == 0 }, 2*time.Second, time.Millisecond)
			require.EqualValues(t, 2, admin.XLen(ctx, prefix+":active").Val(), "保留原stream消息，不通过清空/重建修复")
		})
	}
}
