package mq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSharedKeyModeTransitionCannotRaceLegacyAdmission(t *testing.T) {
	p := newWindowRedisProvider(t)
	legacy := ReliableSubscribeOptions{Group: "g", Consumer: "legacy", KeyConcurrency: 1, Count: 1}
	require.NoError(t, p.ensureReliableGroupMode(context.Background(), "fills", legacy))
	shared := ReliableSubscribeOptions{Group: "g", Consumer: "shared", KeyConcurrency: 1, Count: 4, SharedKeys: true}
	require.ErrorIs(t, p.ensureReliableGroupMode(context.Background(), "fills", shared), ErrReliableGroupModeConflict, "旧订阅已通过启动检查但尚未进入循环时也必须拒绝切换")
}

func TestSharedKeyRedisModesAndSharedBudget(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	opts := ReliableSubscribeOptions{Group: "g", Consumer: "one", KeyConcurrency: 2, Count: 4, SharedKeys: true}
	stop, err := p.SubscribeReliable(ctx, "fills", opts, func(*Message) error { return nil })
	require.NoError(t, err)
	defer stop()
	legacy := opts
	legacy.SharedKeys = false
	_, err = p.SubscribeReliable(ctx, "fills", legacy, func(*Message) error { return nil })
	require.ErrorIs(t, err, ErrReliableGroupModeConflict)
	mismatch := opts
	mismatch.Count = 8
	_, err = p.SubscribeReliable(ctx, "fills", mismatch, func(*Message) error { return nil })
	require.ErrorIs(t, err, ErrReliableGroupModeConflict)
	// 多个原子读取竞争共享预算；handler 被 key lease 挡住，不能靠 ACK 腾出槽位。
	stop()
	p.wg.Wait()
	for i := 0; i < 40; i++ {
		require.NoError(t, p.Publish(ctx, "fills", []byte("x"), &PublishOptions{OrderingKey: fmt.Sprint(i)}))
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			o := opts
			o.Consumer = fmt.Sprint(i)
			_, _ = p.readReliableKeyed(ctx, p.streamKey("fills"), "fills", o, 4)
		}(i)
	}
	wg.Wait()
	require.EqualValues(t, 4, p.client.XPending(ctx, p.streamKey("fills"), "g").Val().Count)
}

func TestSharedKeyRedisLostAckReplyDoesNotReplayAckedWork(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	require.NoError(t, redisReliablePlainAckScript.Load(ctx, p.client).Err())
	hook := &lostAckReplyHook{}
	p.client.AddHook(hook)
	var first, second atomic.Int32
	stop, err := p.SubscribeReliable(ctx, "fills", ReliableSubscribeOptions{Group: "g", Consumer: "c", KeyConcurrency: 2, Count: 4, SharedKeys: true}, func(m *Message) error {
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
		return hook.lost.Load() && second.Load() == 1 && p.client.XPending(ctx, p.streamKey("fills"), "g").Val().Count == 0
	}, 3*time.Second, time.Millisecond)
	require.EqualValues(t, 1, first.Load())
}

func TestSharedKeyRedisLateCompletionCannotAckNewOwner(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(fmt.Sprint(bounded), func(t *testing.T) {
			p := newWindowRedisProvider(t)
			ctx := context.Background()
			opts := ReliableSubscribeOptions{Group: "g", Consumer: "old", KeyConcurrency: 1, Count: 4, SharedKeys: true}
			if bounded {
				opts.lifecycle = &LifecyclePolicy{Retry: RetryPolicy{MaxDeliveries: 1, DeadLetterSubject: "dead"}}
			}
			require.NoError(t, p.ensureReliableGroupMode(ctx, "fills", opts))
			require.NoError(t, p.client.XGroupCreateMkStream(ctx, p.streamKey("fills"), "g", "0").Err())
			require.NoError(t, p.Publish(ctx, "fills", []byte("x"), &PublishOptions{OrderingKey: "A"}))
			entries, err := p.readReliableKeyed(ctx, p.streamKey("fills"), "fills", opts, 1)
			require.NoError(t, err)
			lock := p.sharedOrderingKeyLock("fills", "g", "A")
			err = p.processSharedReliableMessage(ctx, p.streamKey("fills"), "fills", opts, entries[0].Messages[0], func(m *Message) error {
				if m.Ack != nil {
					require.Error(t, m.Ack())
				}
				require.NoError(t, p.client.Set(ctx, lock, "replacement-token", time.Minute).Err())
				if bounded {
					return errors.New("failed")
				}
				return nil
			})
			require.Error(t, err)
			require.EqualValues(t, 1, p.client.XPending(ctx, p.streamKey("fills"), "g").Val().Count)
			require.Equal(t, "replacement-token", p.client.Get(ctx, lock).Val())
			require.Zero(t, p.client.XLen(ctx, p.streamKey("dead")).Val())
		})
	}
}

func TestSharedKeyRedisTimeoutAndCloseWaitForActualHandler(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	entered, hold := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	defer release()
	opts := ReliableSubscribeOptions{Group: "g", Consumer: "c", KeyConcurrency: 1, Count: 4, SharedKeys: true, lifecycle: &LifecyclePolicy{Subject: "fills", RequiredGroups: []ConsumerGroupRequirement{{Name: "g", Start: StartFromAllRetained}}, Retry: RetryPolicy{MaxDeliveries: 2, HandlerTimeout: 20 * time.Millisecond, DeadLetterSubject: "dead"}}}
	stop, err := p.SubscribeReliable(ctx, "fills", opts, func(*Message) error {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-hold
		return nil
	})
	require.NoError(t, err)
	defer stop()
	require.NoError(t, p.Publish(ctx, "fills", []byte("x"), &PublishOptions{OrderingKey: "A"}))
	<-entered
	time.Sleep(100 * time.Millisecond)
	require.EqualValues(t, 1, calls.Load())
	require.EqualValues(t, 1, p.client.XPending(ctx, p.streamKey("fills"), "g").Val().Count)
	stop()
	done := make(chan struct{})
	go func() { _ = p.Close(); close(done) }()
	select {
	case <-done:
		t.Fatal("不能遗弃未退出的 handler")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler 退出后工作者未回收")
	}
	require.EqualValues(t, 1, calls.Load())
}

func TestSharedKeyRedisExposesLeaseAndAckMetrics(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	hold := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(hold) })
	stop, err := p.SubscribeReliable(ctx, "fills", ReliableSubscribeOptions{Group: "g", KeyConcurrency: 1, Count: 4, SharedKeys: true}, func(*Message) error { close(entered); <-hold; return nil })
	require.NoError(t, err)
	defer stop()
	require.NoError(t, p.Publish(ctx, "fills", []byte("x"), &PublishOptions{OrderingKey: "sensitive-user"}))
	<-entered
	snapshot := p.RuntimeMetricSnapshot(ctx)
	require.EqualValues(t, 1, snapshot.Gauges["shared_key_leases"])
	require.Greater(t, snapshot.Counters["shared_key_acquired_total"], float64(0))
	once.Do(func() { close(hold) })
	require.Eventually(t, func() bool { return p.RuntimeMetricSnapshot(ctx).Counters["reliable_ack_total"] == 1 }, time.Second, time.Millisecond)
	require.GreaterOrEqual(t, p.RuntimeMetricSnapshot(ctx).Counters["reliable_ack_seconds_total"], float64(0))
}

func TestSharedKeyRedisConformance(t *testing.T) {
	p := newWindowRedisProvider(t)
	other := NewRedisStreamProvider(p.addr, p.prefix, 0)
	require.NoError(t, other.Connect(context.Background()))
	defer other.Close()
	require.NoError(t, VerifySharedKeyReliableDistribution(p, other))
}

func TestSharedKeyRedisContinuousInputRespectsWorkerAndPrefetchBudgets(t *testing.T) {
	p := newWindowRedisProvider(t)
	other := NewRedisStreamProvider(p.addr, p.prefix, 0)
	ctx := context.Background()
	require.NoError(t, other.Connect(ctx))
	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	t.Cleanup(func() { release(); require.NoError(t, other.Close()) })
	var completed atomic.Int32
	for i, provider := range []*RedisStreamProvider{p, other} {
		stop, err := provider.SubscribeReliable(ctx, "budget", ReliableSubscribeOptions{Group: "g", Consumer: fmt.Sprint(i), SharedKeys: true, KeyConcurrency: 2, Count: 8}, func(*Message) error { <-hold; completed.Add(1); return nil })
		require.NoError(t, err)
		t.Cleanup(stop)
	}
	for i := 0; i < 16; i++ {
		require.NoError(t, p.Publish(ctx, "budget", []byte("x"), &PublishOptions{OrderingKey: fmt.Sprint(i)}))
	}
	require.Eventually(t, func() bool {
		return p.RuntimeMetricSnapshot(ctx).Gauges["handler_inflight"]+other.RuntimeMetricSnapshot(ctx).Gauges["handler_inflight"] == 4
	}, 3*time.Second, time.Millisecond)
	release()
	// 在释放原窗口后持续追加输入，采样实际 worker、排队与共享 PEL 上限。
	for i := 16; i < 200; i++ {
		require.NoError(t, p.Publish(ctx, "budget", []byte("x"), &PublishOptions{OrderingKey: fmt.Sprint(i)}))
		for _, provider := range []*RedisStreamProvider{p, other} {
			snap := provider.RuntimeMetricSnapshot(ctx)
			require.LessOrEqual(t, snap.Gauges["handler_inflight"], float64(2))
			require.LessOrEqual(t, snap.Gauges["pending_keys"], float64(8))
			require.LessOrEqual(t, snap.Gauges["queued_work"], float64(8))
		}
		pending, err := p.client.XPending(ctx, p.streamKey("budget"), "g").Result()
		require.NoError(t, err)
		require.LessOrEqual(t, pending.Count, int64(8))
	}
	require.Eventually(t, func() bool { return completed.Load() == 200 }, 5*time.Second, time.Millisecond)
	for _, provider := range []*RedisStreamProvider{p, other} {
		require.LessOrEqual(t, provider.RuntimeMetricSnapshot(ctx).Gauges["handler_peak"], float64(2))
	}
	// completion 时钟先于 ACK，因此等待 PEL 真正排空。
	require.Eventually(t, func() bool { return p.client.XPending(ctx, p.streamKey("budget"), "g").Val().Count == 0 }, time.Second, time.Millisecond)
}
