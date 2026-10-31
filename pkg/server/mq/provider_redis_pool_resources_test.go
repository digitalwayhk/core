package mq

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisPoolLegacyConstructorUsesFixedDefaults(t *testing.T) {
	for _, cpu := range []int{1, 2} {
		t.Run(fmt.Sprint(cpu), func(t *testing.T) {
			old := runtime.GOMAXPROCS(cpu)
			defer runtime.GOMAXPROCS(old)
			base := newPoolBudgetProvider(t, 0, 0)
			p := NewRedisStreamProvider(base.addr, base.prefix+":legacy", 0)
			require.NoError(t, p.Connect(context.Background()))
			defer p.Close()
			require.Equal(t, config.DefaultRedisStreamCommandPoolSize, p.client.Options().PoolSize)
			require.Equal(t, config.DefaultRedisStreamBlockingReadPoolSize, p.blockingClient.Options().PoolSize)
			require.Equal(t, p.client.Options().PoolSize, p.client.Options().MaxActiveConns)
			require.Equal(t, p.blockingClient.Options().PoolSize, p.blockingClient.Options().MaxActiveConns)
			require.NotSame(t, p.client, p.blockingClient)
		})
	}
}

func TestRedisPoolSubscriptionChurnIsBounded(t *testing.T) {
	p := newPoolBudgetProvider(t, 2, 4)
	baseline := runtime.NumGoroutine()
	ctx := context.Background()
	for round := 0; round < 3; round++ {
		stops := make([]func(), 0, 64)
		for i := 0; i < 64; i++ {
			stop, err := p.Subscribe(ctx, fmt.Sprintf("idle-%d-%d", round, i), func(*Message) {})
			require.NoError(t, err)
			stops = append(stops, stop)
		}
		require.LessOrEqual(t, p.client.PoolStats().TotalConns, uint32(2))
		require.LessOrEqual(t, p.blockingClient.PoolStats().TotalConns, uint32(4))
		for _, stop := range stops {
			stop()
		}
		p.mu.Lock()
		entries := len(p.subs)
		p.mu.Unlock()
		require.Zero(t, entries)
		joined := make(chan struct{})
		go func() { p.wg.Wait(); close(joined) }()
		select {
		case <-joined:
		case <-time.After(3 * time.Second):
			t.Fatal("取消后仍遗留读取工作者")
		}
		require.Eventually(t, func() bool { return runtime.NumGoroutine() <= baseline+20 }, time.Second, time.Millisecond, "后台连接任务应随固定池预算有界，而非随64个已取消订阅增长")
	}
	require.NoError(t, p.Close())
	require.Zero(t, p.client.PoolStats().TotalConns)
	require.Zero(t, p.blockingClient.PoolStats().TotalConns)
	t.Logf("三轮各64订阅取消后，goroutine baseline=%d final=%d，两个池已归零", baseline, runtime.NumGoroutine())
}

func TestRedisPoolConcurrentSubscribeCloseDoesNotLeakWorkers(t *testing.T) {
	p := newPoolBudgetProvider(t, 2, 3)
	var callers sync.WaitGroup
	for i := 0; i < 30; i++ {
		callers.Add(1)
		go func(i int) {
			defer callers.Done()
			stop, err := p.SubscribeReliable(context.Background(), fmt.Sprintf("closing-%d", i), ReliableSubscribeOptions{Group: "g", KeyConcurrency: 2}, func(*Message) error { return nil })
			if err == nil {
				stop()
			}
		}(i)
	}
	require.NoError(t, p.Close())
	callers.Wait()
	p.wg.Wait()
	p.mu.Lock()
	entries := len(p.subs)
	p.mu.Unlock()
	require.Zero(t, entries)
	require.Zero(t, p.client.PoolStats().TotalConns)
	require.Zero(t, p.blockingClient.PoolStats().TotalConns)
}

func TestRedisPoolSlowHandlerCancellationRetainsPending(t *testing.T) {
	for _, n := range []int{1, 2} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			p := newPoolBudgetProvider(t, 2, 3)
			ctx := context.Background()
			entered, hold := make(chan struct{}), make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(hold) }) }
			defer release()
			stop, err := p.SubscribeReliable(ctx, "slow", ReliableSubscribeOptions{Group: "g", KeyConcurrency: n}, func(*Message) error { close(entered); <-hold; return nil })
			require.NoError(t, err)
			require.NoError(t, p.Publish(ctx, "slow", []byte("work"), &PublishOptions{OrderingKey: "A"}))
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("handler未开始")
			}
			stop()
			done := make(chan error, 1)
			go func() { done <- p.Close() }()
			select {
			case <-done:
				t.Fatal("不能把取消当成在途handler已经退出")
			case <-time.After(30 * time.Millisecond):
			}
			release()
			require.NoError(t, <-done)
			// 独立管理连接检查Broker：取消后成功迟到不能ACK。
			admin := redis.NewClient(&redis.Options{Addr: p.addr, PoolSize: 1})
			defer admin.Close()
			pending, err := admin.XPending(ctx, p.streamKey("slow"), "g").Result()
			require.NoError(t, err)
			require.EqualValues(t, 1, pending.Count)
		})
	}
}

func TestRedisPoolRegisteredReadersReflectScheduling(t *testing.T) {
	p := newPoolBudgetProvider(t, 2, 3)
	for _, n := range []int{1, 2} {
		stop, err := p.SubscribeReliable(context.Background(), fmt.Sprint(n), ReliableSubscribeOptions{Group: "g", KeyConcurrency: n}, func(*Message) error { return nil })
		require.NoError(t, err)
		defer stop()
	}
	stop, err := p.Subscribe(context.Background(), "ordinary", func(*Message) {})
	require.NoError(t, err)
	defer stop()
	require.EqualValues(t, 2, p.RuntimeMetricSnapshot(context.Background()).Gauges["redis_blocking_subscriptions"])
}

// 父context取消也应清登记；调用方不必再调用返回的stop才能释放观测对象。
func TestRedisPoolParentCancellationRemovesRegistration(t *testing.T) {
	for _, n := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			p := newPoolBudgetProvider(t, 2, 3)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var stop func()
			var err error
			if n == 0 {
				stop, err = p.Subscribe(ctx, "parent", func(*Message) {})
			} else {
				stop, err = p.SubscribeReliable(ctx, "parent", ReliableSubscribeOptions{Group: "g", KeyConcurrency: n}, func(*Message) error { return nil })
			}
			require.NoError(t, err)
			defer stop()
			cancel()
			joined := make(chan struct{})
			go func() { p.wg.Wait(); close(joined) }()
			select {
			case <-joined:
			case <-time.After(3 * time.Second):
				t.Fatal("父context取消后工作者未退出")
			}
			p.mu.Lock()
			entries, metrics := len(p.subs), len(p.reliableMetrics)
			p.mu.Unlock()
			require.Zero(t, entries, "退出后不能保留订阅登记")
			require.Zero(t, metrics, "退出后不能保留订阅观测对象")
		})
	}
}

func TestRedisPoolDefaultBudgetFeedsSerialWithTwelveIdleSubjects(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	p := newPoolBudgetProvider(t, 0, 0)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		stop, err := p.SubscribeReliable(ctx, fmt.Sprintf("idle-default-%d", i), ReliableSubscribeOptions{Group: "g"}, func(*Message) error { return nil })
		require.NoError(t, err)
		defer stop()
	}
	for i := 0; i < 64; i++ {
		require.NoError(t, p.Publish(ctx, "active-default", []byte(fmt.Sprint(i)), &PublishOptions{OrderingKey: "A"}))
	}
	received := make(chan string, 64)
	stop, err := p.SubscribeReliable(ctx, "active-default", ReliableSubscribeOptions{Group: "g"}, func(m *Message) error { received <- string(m.Data); return nil })
	require.NoError(t, err)
	defer stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for i := 0; i < 64; i++ {
		select {
		case value := <-received:
			require.Equal(t, fmt.Sprint(i), value)
		case <-deadline.C:
			t.Fatal("预算覆盖13个阻塞读取时，活跃串行主题应持续推进")
		}
	}
	require.EqualValues(t, 13, p.RuntimeMetricSnapshot(ctx).Gauges["redis_blocking_subscriptions"])
}
