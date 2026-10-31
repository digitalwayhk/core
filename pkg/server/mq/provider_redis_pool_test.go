package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// JSON 允许同一用例在旧版本忽略新预算字段后仍真实运行，而非编译失败。
func newPoolBudgetProvider(t *testing.T, commands, blocking int) *RedisStreamProvider {
	t.Helper()
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR")
	}
	var cfg config.MQConfig
	data, _ := json.Marshal(map[string]any{"Mode": "on", "Provider": "redis-stream", "Usage": []string{"event-stream"}, "RedisStream": map[string]any{"Addr": addr, "Prefix": fmt.Sprintf("core:pool:%d", time.Now().UnixNano()), "CommandPoolSize": commands, "BlockingReadPoolSize": blocking}})
	require.NoError(t, json.Unmarshal(data, &cfg))
	cfg.ApplyDefaults()
	manager, err := BuildManager(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			require.ErrorIs(t, err, redis.ErrClosed)
		}
	})
	return manager.Current().(*RedisStreamProvider)
}

func blockedRedisReaders(t *testing.T, admin *redis.Client) int {
	t.Helper()
	result, err := admin.ClientList(context.Background()).Result()
	require.NoError(t, err)
	n := 0
	for _, line := range strings.Split(result, "\n") {
		if strings.Contains(line, "cmd=xreadgroup") && strings.Contains(line, "flags=b") {
			n++
		}
	}
	return n
}

func TestRedisPoolBlockingReadsCannotStarveCommands(t *testing.T) {
	for _, cpu := range []int{1, 2} {
		t.Run(fmt.Sprint(cpu), func(t *testing.T) {
			old := runtime.GOMAXPROCS(cpu)
			defer runtime.GOMAXPROCS(old)
			// 前后使用相同总预算10*CPU和相同订阅数；新配置只拆分，不增加总额。
			total, commands := 10*cpu, 2
			reads := total - commands
			p := newPoolBudgetProvider(t, commands, reads)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			admin := redis.NewClient(&redis.Options{Addr: os.Getenv("CORE_TEST_REDIS_ADDR"), PoolSize: 1, MaxActiveConns: 1})
			defer admin.Close()
			opts := ReliableSubscribeOptions{Group: "active", Consumer: "active", KeyConcurrency: 2}
			require.True(t, p.tryAcquireOwner(ctx, "active", opts))
			require.NoError(t, p.client.XGroupCreateMkStream(ctx, p.streamKey("active"), opts.Group, "0").Err())
			require.NoError(t, p.Publish(ctx, "active", []byte("work"), &PublishOptions{OrderingKey: "A"}))
			entries, err := p.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: opts.Group, Consumer: opts.Consumer, Streams: []string{p.streamKey("active"), ">"}, Count: 1, Block: -1}).Result()
			require.NoError(t, err)
			// 并发启动真实普通订阅，Broker CLIENT LIST 确认池已被长轮询占满。
			var started sync.WaitGroup
			for i := 0; i < total; i++ {
				started.Add(1)
				go func(i int) {
					defer started.Done()
					_, err := p.Subscribe(ctx, fmt.Sprintf("idle-%d", i), func(*Message) {})
					if err != nil {
						t.Errorf("subscribe: %v", err)
					}
				}(i)
			}
			ready := make(chan struct{})
			go func() { started.Wait(); close(ready) }()
			select {
			case <-ready:
			case <-time.After(12 * time.Second):
				t.Fatal("订阅启动未完成")
			}
			expected := total
			if p.client.Options().PoolSize == commands {
				expected = reads
			}
			require.Eventually(t, func() bool { return blockedRedisReaders(t, admin) == expected }, 4*time.Second, time.Millisecond, "必须先确认真实Broker阻塞连接饱和")
			// 500ms仅为同步夹具的失活保护，小于本例既有2s BLOCK；不是业务性能门槛。
			commandCtx, stop := context.WithTimeout(ctx, 500*time.Millisecond)
			defer stop()
			results := make(chan error, 4)
			go func() {
				if !p.renewOwner(commandCtx, "active", opts) {
					results <- fmt.Errorf("renew owner failed while reads blocked")
				} else {
					results <- nil
				}
			}()
			go func() {
				if !p.stillOwner(commandCtx, "active", opts) {
					results <- fmt.Errorf("owner check failed while reads blocked")
				} else {
					results <- nil
				}
			}()
			go func() { results <- p.Publish(commandCtx, "new", []byte("publish"), nil) }()
			go func() {
				results <- p.processReliableMessage(commandCtx, p.streamKey("active"), "active", opts, entries[0].Messages[0], func(*Message) error { return nil })
			}()
			for i := 0; i < 4; i++ {
				if err := <-results; err != nil {
					t.Errorf("控制命令不应等待空闲主题长轮询释放连接: %v", err)
				}
			}
			if t.Failed() {
				return
			}
			require.Greater(t, blockedRedisReaders(t, admin), 0, "控制命令完成时空闲读取仍应阻塞")
			require.Zero(t, p.client.XPending(ctx, p.streamKey("active"), opts.Group).Val().Count, "只有handler成功并fenced ACK后pending才清空")
		})
	}
}

func TestRedisPoolFactoryUsesExplicitCommandBudget(t *testing.T) {
	p := newPoolBudgetProvider(t, 3, 5)
	require.Equal(t, 3, p.client.Options().PoolSize)
	require.Equal(t, 3, p.client.Options().MaxActiveConns)
}

func TestRedisPoolMetricsAreCollectedAndAbsentBeforeConnect(t *testing.T) {
	p := NewRedisStreamProvider("unused", "unused", 0)
	snapshot := p.RuntimeMetricSnapshot(context.Background())
	_, exists := snapshot.Gauges["redis_command_pool_total"]
	require.False(t, exists, "未采集不能填零")
	p = newPoolBudgetProvider(t, 3, 5)
	snapshot = p.RuntimeMetricSnapshot(context.Background())
	_, exists = snapshot.Gauges["redis_command_pool_total"]
	require.True(t, exists, "必须采集实际PoolStats")
}

func TestRedisPoolCloseInterruptsBlockedRead(t *testing.T) {
	p := newPoolBudgetProvider(t, 2, 3)
	ctx := context.Background()
	_, err := p.Subscribe(ctx, "idle", func(*Message) {})
	require.NoError(t, err)
	admin := redis.NewClient(&redis.Options{Addr: os.Getenv("CORE_TEST_REDIS_ADDR"), PoolSize: 1})
	defer admin.Close()
	require.Eventually(t, func() bool { return blockedRedisReaders(t, admin) > 0 }, time.Second, time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- p.Close() }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Error("Close 应关闭阻塞读池打断长轮询，不等待既有2秒BLOCK自然结束")
		require.NoError(t, <-done)
	}
}

func TestRedisPoolCancelRemovesSubscriptionRegistration(t *testing.T) {
	p := newPoolBudgetProvider(t, 2, 3)
	stop, err := p.Subscribe(context.Background(), "idle", func(*Message) {})
	require.NoError(t, err)
	stop()
	p.mu.Lock()
	remaining := len(p.subs)
	p.mu.Unlock()
	require.Zero(t, remaining, "取消后不保留失效的订阅登记")
}
