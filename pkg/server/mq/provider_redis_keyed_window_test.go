package mq

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newWindowRedisProvider(t *testing.T) *RedisStreamProvider {
	t.Helper()
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR")
	}
	p := NewRedisStreamProvider(addr, fmt.Sprintf("core:window:%d", time.Now().UnixNano()), 0)
	require.NoError(t, p.Connect(context.Background()))
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			require.ErrorIs(t, err, redis.ErrClosed)
		}
	})
	return p
}

func TestRedisReliableKeyedNextWindowDoesNotWaitForUnrelatedKey(t *testing.T) {
	for _, reclaim := range []bool{false, true} {
		t.Run(fmt.Sprintf("reclaim=%v", reclaim), func(t *testing.T) {
			p := newWindowRedisProvider(t)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			hold := make(chan struct{})
			defer close(hold)
			aStarted, bFirst, bNext := make(chan struct{}), make(chan struct{}), make(chan struct{})
			publish := func(key, body string) {
				require.NoError(t, p.Publish(ctx, "fills", []byte(body), &PublishOptions{OrderingKey: key}))
			}
			publish("A", "A1")
			publish("B", "B1")
			if reclaim {
				require.NoError(t, p.client.XGroupCreate(ctx, p.streamKey("fills"), "g", "0").Err())
				_, err := p.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "g", Consumer: "crashed", Streams: []string{p.streamKey("fills"), ">"}, Count: 2}).Result()
				require.NoError(t, err)
				publish("B", "B2")
				_, err = p.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "g", Consumer: "crashed", Streams: []string{p.streamKey("fills"), ">"}, Count: 1}).Result()
				require.NoError(t, err)
			}
			stop, err := p.SubscribeReliable(ctx, "fills", ReliableSubscribeOptions{Group: "g", Consumer: "live", KeyConcurrency: 2, Count: 2}, func(m *Message) error {
				switch string(m.Data) {
				case "A1":
					close(aStarted)
					<-hold
				case "B1":
					close(bFirst)
				case "B2":
					close(bNext)
				}
				return nil
			})
			require.NoError(t, err)
			defer stop()
			for _, signal := range []chan struct{}{aStarted, bFirst} {
				select {
				case <-signal:
				case <-ctx.Done():
					t.Fatal("首窗口没有进入两个独立 key")
				}
			}
			if !reclaim {
				publish("B", "B2")
			}
			select {
			case <-bNext:
			case <-time.After(time.Second):
				t.Fatal("B1 已完成，但 B2 跨读取窗口仍等待阻塞的 A1")
			}
		})
	}
}

func TestRedisReliableHandlerCannotReacquireLostOwnerToAck(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	options := ReliableSubscribeOptions{Group: "g", Consumer: "old", MinIdle: time.Second}
	key := p.streamKey("fills")
	require.NoError(t, p.client.XGroupCreateMkStream(ctx, key, "g", "0").Err())
	id, err := p.client.XAdd(ctx, &redis.XAddArgs{Stream: key, Values: map[string]interface{}{"data": "x"}}).Result()
	require.NoError(t, err)
	entries, err := p.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "g", Consumer: "old", Streams: []string{key, ">"}, Count: 1}).Result()
	require.NoError(t, err)
	require.True(t, p.tryAcquireOwner(ctx, "fills", options))
	err = p.processReliableMessage(ctx, key, "fills", options, entries[0].Messages[0], func(*Message) error {
		return p.client.Del(ctx, p.ownerLockKey("fills", "g")).Err()
	})
	require.Error(t, err, "失去 owner 的旧工作不得重新抢占并 ACK")
	pending, err := p.client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: key, Group: "g", Start: id, End: id, Count: 1}).Result()
	require.NoError(t, err)
	require.Len(t, pending, 1)
}

func TestRedisReliableKeyedTimeoutWaitsForHandlerBeforeRetry(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hold := make(chan struct{})
	defer close(hold)
	var calls atomic.Int32
	opts := ReliableSubscribeOptions{Group: "g", Consumer: "c", KeyConcurrency: 2, Count: 4,
		lifecycle: &LifecyclePolicy{Retry: RetryPolicy{MaxDeliveries: 10, HandlerTimeout: 20 * time.Millisecond, Backoff: []time.Duration{10 * time.Millisecond}}}}
	key := p.streamKey("fills")
	require.NoError(t, p.client.XGroupCreateMkStream(ctx, key, "g", "0").Err())
	require.True(t, p.tryAcquireOwner(ctx, "fills", opts))
	require.NoError(t, p.Publish(ctx, "fills", []byte("x"), &PublishOptions{OrderingKey: "A"}))
	metrics := &redisReliableSubscriptionMetrics{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.runReliableKeyedOwner(ctx, key, "fills", opts, func(*Message) error { calls.Add(1); <-hold; return nil }, metrics)
	}()
	t.Cleanup(func() { cancel(); <-done })
	require.Eventually(t, func() bool { return calls.Load() > 0 }, time.Second, time.Millisecond)
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, int32(1), calls.Load(), "超时 handler 尚未退出，不得重复创建 handler 或并行重投")
}

func TestRedisReliableKeyedAcrossPagesKeepsOrderAndFailureBarrier(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	const perKey = 24
	key := p.streamKey("fills")
	// 制造比新预取预算更大的既有 PEL，覆盖接管、分页与完成后回扫。
	for n := 0; n < perKey; n++ {
		for _, k := range []string{"A", "B", "C"} {
			require.NoError(t, p.Publish(ctx, "fills", []byte(fmt.Sprintf("%s:%d", k, n)), &PublishOptions{OrderingKey: k}))
		}
	}
	require.NoError(t, p.client.XGroupCreate(ctx, key, "g", "0").Err())
	_, err := p.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "g", Consumer: "crashed", Streams: []string{key, ">"}, Count: 3 * perKey}).Result()
	require.NoError(t, err)
	var allowA atomic.Bool
	var mu sync.Mutex
	next := map[string]int{}
	active := map[string]int{}
	seen := map[string]bool{}
	violations := []string{}
	stop, err := p.SubscribeReliable(ctx, "fills", ReliableSubscribeOptions{Group: "g", Consumer: "live", Count: 4, KeyConcurrency: 3}, func(m *Message) error {
		var k string
		var n int
		_, scanErr := fmt.Sscanf(string(m.Data), "%1s:%d", &k, &n)
		if scanErr != nil {
			return scanErr
		}
		if k == "A" && n == 0 && !allowA.Load() {
			return errors.New("temporary failure")
		}
		mu.Lock()
		active[k]++
		if active[k] != 1 || next[k] != n || seen[m.ID] {
			violations = append(violations, string(m.Data))
		}
		mu.Unlock()
		time.Sleep(time.Millisecond)
		mu.Lock()
		next[k]++
		active[k]--
		seen[m.ID] = true
		mu.Unlock()
		return nil
	})
	require.NoError(t, err)
	defer stop()
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return next["B"] == perKey && next["C"] == perKey
	}, 6*time.Second, 5*time.Millisecond)
	mu.Lock()
	aBefore := next["A"]
	mu.Unlock()
	require.Zero(t, aBefore, "A 的失败头不得被 A 的后继越过")
	allowA.Store(true)
	require.Eventually(t, func() bool { return p.client.XPending(ctx, key, "g").Val().Count == 0 }, 5*time.Second, 5*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	require.Empty(t, violations)
	require.Equal(t, perKey, next["A"])
	require.Len(t, seen, 3*perKey)
}

func TestRedisReliableKeyedSustainedInputBoundsPrefetchAndWorkers(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hold := make(chan struct{})
	defer close(hold)
	var inflight, peak atomic.Int32
	stop, err := p.SubscribeReliable(ctx, "fills", ReliableSubscribeOptions{Group: "g", Consumer: "live", KeyConcurrency: 2, Count: 8}, func(*Message) error {
		n := inflight.Add(1)
		defer inflight.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		<-hold
		return nil
	})
	require.NoError(t, err)
	defer stop()
	for n := 0; n < 200; n++ {
		require.NoError(t, p.Publish(ctx, "fills", []byte("x"), &PublishOptions{OrderingKey: fmt.Sprint(n)}))
	}
	require.Eventually(t, func() bool { return inflight.Load() == 2 }, time.Second, time.Millisecond)
	for n := 0; n < 40; n++ {
		pending, err := p.client.XPending(ctx, p.streamKey("fills"), "g").Result()
		require.NoError(t, err)
		require.LessOrEqual(t, pending.Count, int64(8))
		snapshot := p.RuntimeMetricSnapshot(ctx)
		require.LessOrEqual(t, snapshot.Gauges["pending_keys"], float64(8))
		require.LessOrEqual(t, snapshot.Gauges["queued_work"], float64(6))
		require.LessOrEqual(t, snapshot.Gauges["handler_inflight"], float64(2))
		require.NoError(t, p.Publish(ctx, "fills", []byte("more"), &PublishOptions{OrderingKey: fmt.Sprint(200 + n)}))
		time.Sleep(5 * time.Millisecond)
	}
	require.Equal(t, int32(2), peak.Load())
	require.Equal(t, int64(8), p.client.XPending(ctx, p.streamKey("fills"), "g").Val().Count)
}

func TestRedisReliableKeyedCancelAndOwnerChangeRecoverWithoutAckedReplay(t *testing.T) {
	for _, loseOwner := range []bool{false, true} {
		t.Run(fmt.Sprintf("owner_change=%v", loseOwner), func(t *testing.T) {
			p := newWindowRedisProvider(t)
			ctx := context.Background()
			opts := ReliableSubscribeOptions{Group: "g", Consumer: "old", KeyConcurrency: 2, Count: 4}
			hold, entered := make(chan struct{}), make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(hold) }) }
			defer release()
			var ackedCalls atomic.Int32
			stop, err := p.SubscribeReliable(ctx, "fills", opts, func(m *Message) error {
				if string(m.Data) == "acked" {
					ackedCalls.Add(1)
					return nil
				}
				close(entered)
				<-hold
				return nil
			})
			require.NoError(t, err)
			require.NoError(t, p.Publish(ctx, "fills", []byte("acked"), &PublishOptions{OrderingKey: "B"}))
			require.Eventually(t, func() bool {
				return ackedCalls.Load() == 1 && p.client.XPending(ctx, p.streamKey("fills"), "g").Val().Count == 0
			}, time.Second, time.Millisecond)
			require.NoError(t, p.Publish(ctx, "fills", []byte("pending"), &PublishOptions{OrderingKey: "A"}))
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("handler 未开始")
			}
			if loseOwner {
				require.NoError(t, p.client.Set(ctx, p.ownerLockKey("fills", "g"), "replacement", time.Minute).Err())
			}
			stop()
			closed := make(chan error, 1)
			go func() { closed <- p.Close() }()
			select {
			case <-closed:
				t.Fatal("Close 遗弃了仍在执行的 handler")
			case <-time.After(40 * time.Millisecond):
			}
			release()
			select {
			case err := <-closed:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("handler 退出后仍遗留 worker")
			}
			restarted := NewRedisStreamProvider(p.addr, p.prefix, 0)
			require.NoError(t, restarted.Connect(ctx))
			defer restarted.Close()
			require.Equal(t, int64(1), restarted.client.XPending(ctx, p.streamKey("fills"), "g").Val().Count)
			if loseOwner {
				require.Equal(t, "replacement", restarted.client.Get(ctx, p.ownerLockKey("fills", "g")).Val(), "旧 owner 退出不得删新 owner 锁")
				require.NoError(t, restarted.client.Del(ctx, p.ownerLockKey("fills", "g")).Err())
			}
			var recovered atomic.Int32
			opts.Consumer = "new"
			stopNew, err := restarted.SubscribeReliable(ctx, "fills", opts, func(m *Message) error {
				if string(m.Data) == "acked" {
					ackedCalls.Add(1)
				} else {
					recovered.Add(1)
				}
				return nil
			})
			require.NoError(t, err)
			defer stopNew()
			require.Eventually(t, func() bool {
				return recovered.Load() == 1 && restarted.client.XPending(ctx, p.streamKey("fills"), "g").Val().Count == 0
			}, time.Second, time.Millisecond)
			require.Equal(t, int32(1), ackedCalls.Load())
		})
	}
}

func TestRedisReliableKeyedFencesReadClaimAckAndDeadLetter(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	key := p.streamKey("fills")
	opts := ReliableSubscribeOptions{Group: "g", Consumer: "old", KeyConcurrency: 2}
	require.NoError(t, p.client.XGroupCreateMkStream(ctx, key, "g", "0").Err())
	require.NoError(t, p.Publish(ctx, "fills", []byte("pending"), &PublishOptions{OrderingKey: "A"}))
	entries, err := p.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "g", Consumer: "replacement", Streams: []string{key, ">"}, Count: 1}).Result()
	require.NoError(t, err)
	require.NoError(t, p.Publish(ctx, "fills", []byte("unread"), &PublishOptions{OrderingKey: "B"}))
	require.NoError(t, p.client.Set(ctx, p.ownerLockKey("fills", "g"), "replacement", time.Minute).Err())
	_, _, err = p.claimReliableKeyed(ctx, key, "fills", opts, "0-0", 4)
	require.ErrorContains(t, err, "RELIABLE_OWNER_LOST")
	_, err = p.readReliableKeyed(ctx, key, "fills", opts, 4)
	require.ErrorContains(t, err, "RELIABLE_OWNER_LOST")
	for _, policy := range []*LifecyclePolicy{nil, {Retry: RetryPolicy{MaxDeliveries: 1, DeadLetterSubject: "dead"}}} {
		opts.lifecycle = policy
		err = p.processReliableMessage(ctx, key, "fills", opts, entries[0].Messages[0], func(m *Message) error {
			if m.Ack != nil {
				require.Error(t, m.Ack(), "可靠 handler 不得提前 ACK")
			}
			return errors.New("failure")
		})
		require.Error(t, err)
	}
	pending, err := p.client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: key, Group: "g", Start: "-", End: "+", Count: 4}).Result()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "replacement", pending[0].Consumer)
	require.Zero(t, p.client.XLen(ctx, p.streamKey("dead")).Val())
}

func TestRedisReliableKeyedOwnerLossStopsSessionWithoutCancel(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts := ReliableSubscribeOptions{Group: "g", Consumer: "old", KeyConcurrency: 2, Count: 4}
	key := p.streamKey("fills")
	require.NoError(t, p.client.XGroupCreateMkStream(ctx, key, "g", "0").Err())
	require.True(t, p.tryAcquireOwner(ctx, "fills", opts))
	require.NoError(t, p.Publish(ctx, "fills", []byte("x"), &PublishOptions{OrderingKey: "A"}))
	hold, entered, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	defer release()
	go func() {
		defer close(done)
		p.runReliableKeyedOwner(ctx, key, "fills", opts, func(*Message) error { close(entered); <-hold; return nil }, &redisReliableSubscriptionMetrics{})
	}()
	t.Cleanup(func() { cancel(); release(); <-done })
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler 未开始")
	}
	require.NoError(t, p.client.Set(ctx, p.ownerLockKey("fills", "g"), "replacement", time.Minute).Err())
	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("owner 丢失后会话未退出")
	}
	require.Equal(t, int64(1), p.client.XPending(ctx, key, "g").Val().Count)
	require.Equal(t, "replacement", p.client.Get(ctx, p.ownerLockKey("fills", "g")).Val())
}

type lostAckReplyHook struct{ lost atomic.Bool }

func (*lostAckReplyHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) { return next(ctx, network, addr) }
}
func (*lostAckReplyHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *lostAckReplyHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		args := cmd.Args()
		if err == nil && len(args) > 1 && args[1] == redisReliablePlainAckScript.Hash() && h.lost.CompareAndSwap(false, true) {
			return errors.New("test: ACK response lost after Redis applied it")
		}
		return err
	}
}

func TestRedisReliableKeyedLostAckReplyDoesNotReplayAlreadyAckedWork(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	require.NoError(t, redisReliablePlainAckScript.Load(ctx, p.client).Err())
	hook := &lostAckReplyHook{}
	p.client.AddHook(hook)
	var first, second atomic.Int32
	stop, err := p.SubscribeReliable(ctx, "fills", ReliableSubscribeOptions{Group: "g", Consumer: "c", KeyConcurrency: 2, Count: 4}, func(m *Message) error {
		if string(m.Data) == "first" {
			first.Add(1)
		} else {
			second.Add(1)
		}
		return nil
	})
	require.NoError(t, err)
	defer stop()
	for _, body := range []string{"first", "second"} {
		require.NoError(t, p.Publish(ctx, "fills", []byte(body), &PublishOptions{OrderingKey: "A"}))
	}
	require.Eventually(t, func() bool {
		return second.Load() == 1 && p.client.XPending(ctx, p.streamKey("fills"), "g").Val().Count == 0
	}, 2*time.Second, time.Millisecond)
	require.True(t, hook.lost.Load())
	require.Equal(t, int32(1), first.Load(), "ACK 已执行，仅响应丢失；调度器必须复核 PEL 后释放失败屏障")
}

func TestRedisReliableKeyedPendingAndNewReadFairness(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var failures, completed atomic.Int32
	stop, err := p.SubscribeReliable(ctx, "fills", ReliableSubscribeOptions{Group: "g", KeyConcurrency: 2, Count: 8}, func(m *Message) error {
		if string(m.Data) == "poison" {
			failures.Add(1)
			return errors.New("temporary failure")
		}
		completed.Add(1)
		return nil
	})
	require.NoError(t, err)
	defer stop()
	require.NoError(t, p.Publish(ctx, "fills", []byte("poison"), &PublishOptions{OrderingKey: "A"}))
	require.Eventually(t, func() bool { return failures.Load() > 0 }, time.Second, time.Millisecond)
	// 不断生成新消息，同时旧 PEL 始终非空；两条路径都必须有进展。
	for n := int32(1); n <= 32; n++ {
		require.NoError(t, p.Publish(ctx, "fills", []byte("independent"), &PublishOptions{OrderingKey: "B"}))
		require.Eventually(t, func() bool { return completed.Load() == n }, time.Second, time.Millisecond)
	}
	require.Eventually(t, func() bool { return failures.Load() >= 2 }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return p.client.XPending(ctx, p.streamKey("fills"), "g").Val().Count == 1 }, time.Second, time.Millisecond)
	metrics := p.RuntimeMetricSnapshot(ctx)
	require.Equal(t, float64(33), metrics.Counters["reliable_new_read_total"])
	require.Greater(t, metrics.Counters["reliable_reclaim_read_total"], float64(0))
	require.Greater(t, metrics.Counters["reliable_handler_seconds_total"], float64(0))
	stop()
	require.Equal(t, metrics.Counters["reliable_new_read_total"], p.RuntimeMetricSnapshot(ctx).Counters["reliable_new_read_total"], "取消订阅不得使 provider 累计计数回退")
}

func TestRedisReliableKeyedCrashFixture(t *testing.T) {
	prefix := os.Getenv("CORE_REDIS_WINDOW_CRASH_PREFIX")
	if prefix == "" {
		t.Skip("子进程崩溃 fixture，仅由恢复测试启动")
	}
	p := NewRedisStreamProvider(os.Getenv("CORE_TEST_REDIS_ADDR"), prefix, 0)
	require.NoError(t, p.Connect(context.Background()))
	_, err := p.SubscribeReliable(context.Background(), "fills", ReliableSubscribeOptions{Group: "g", Consumer: "crashed", KeyConcurrency: 2, Count: 4}, func(*Message) error {
		os.Exit(73) // 不运行 defer/Close，不 ACK，保留真实 PEL 和 owner lease。
		return nil
	})
	require.NoError(t, err)
	select {}
}

func TestRedisReliableKeyedActualProcessCrashRecoveryAndLegalDuplicate(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	require.NoError(t, p.Publish(ctx, "fills", []byte("event-1"), &PublishOptions{OrderingKey: "A", IdempotencyKey: "event-1"}))
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRedisReliableKeyedCrashFixture$")
	command.Env = append(os.Environ(), "CORE_REDIS_WINDOW_CRASH_PREFIX="+p.prefix)
	err := command.Run()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 73, exit.ExitCode())
	require.Equal(t, int64(1), p.client.XPending(ctx, p.streamKey("fills"), "g").Val().Count)
	// 保留真实崩溃的租约，单独加速 Redis TTL 到期，避免每次回归等待 2 分钟。
	require.True(t, p.client.PExpire(ctx, p.ownerLockKey("fills", "g"), time.Millisecond).Val())
	require.Eventually(t, func() bool { return p.client.Exists(ctx, p.ownerLockKey("fills", "g")).Val() == 0 }, time.Second, time.Millisecond)
	var mu sync.Mutex
	processed := map[string]bool{}
	deliveries, writes := 0, 0
	stop, err := p.SubscribeReliable(ctx, "fills", ReliableSubscribeOptions{Group: "g", Consumer: "restarted", KeyConcurrency: 2, Count: 4}, func(m *Message) error {
		mu.Lock()
		defer mu.Unlock()
		deliveries++
		// 合法重复仍须由消费方沿用事件身份幂等，Core 不猜测业务去重策略。
		if !processed[string(m.Data)] {
			processed[string(m.Data)] = true
			writes++
		}
		return nil
	})
	require.NoError(t, err)
	defer stop()
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return deliveries == 1 }, 2*time.Second, time.Millisecond)
	require.NoError(t, p.Publish(ctx, "fills", []byte("event-1"), &PublishOptions{OrderingKey: "A", IdempotencyKey: "event-1"}))
	require.Eventually(t, func() bool {
		mu.Lock()
		n := deliveries
		mu.Unlock()
		return n == 2 && p.client.XPending(ctx, p.streamKey("fills"), "g").Val().Count == 0
	}, 2*time.Second, time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 2, deliveries)
	require.Equal(t, 1, writes)
}
