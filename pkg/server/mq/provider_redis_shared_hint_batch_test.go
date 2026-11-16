// 本文件验证原PEL窗口内候选正文只需一次有界网络往返，保留实际handler/ACK和同key前序。
package mq

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type sharedHintRoundTripHook struct {
	mu                           sync.Mutex
	current, max, total, batches int
	started                      *atomic.Int32
	once                         sync.Once
	blocked, allow               chan struct{}
}

// DialHook透传原连接。
func (*sharedHintRoundTripHook) DialHook(n redis.DialHook) redis.DialHook { return n }
func sharedHintBodyCommand(cmd redis.Cmder) bool {
	a := cmd.Args()
	return len(a) == 5 && cmd.Name() == "eval" && strings.Contains(fmt.Sprint(a[1]), "SHARED_PENDING_BODY_MISSING")
}
func (h *sharedHintRoundTripHook) body(n int) {
	if n == 0 {
		return
	}
	h.mu.Lock()
	h.current++
	h.total += n
	if n > 1 {
		h.batches++
	}
	if h.current > h.max {
		h.max = h.current
	}
	h.mu.Unlock()
}

// ProcessHook以真实PEL命令控制屏障并计数串行往返。
func (h *sharedHintRoundTripHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		a := cmd.Args()
		if len(a) > 1 && a[1] == redisSharedPendingHintScript {
			h.mu.Lock()
			h.current = 0
			h.mu.Unlock()
			if h.started.Load() == 8 {
				h.once.Do(func() { close(h.blocked); <-h.allow })
			}
		}
		if sharedHintBodyCommand(cmd) {
			h.body(1)
		}
		return next(ctx, cmd)
	}
}

// ProcessPipelineHook把一批实际正文命令计为一次客户端往返。
func (h *sharedHintRoundTripHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		n := 0
		for _, cmd := range cmds {
			if sharedHintBodyCommand(cmd) {
				n++
			}
		}
		h.body(n)
		return next(ctx, cmds)
	}
}

// TestRedisSharedHintCandidatesUseOneBoundedRoundTrip以实际框架入口固定8个头完成，再恢复同一原PEL页。
func TestRedisSharedHintCandidatesUseOneBoundedRoundTrip(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	var started atomic.Int32
	hook := &sharedHintRoundTripHook{started: &started, blocked: make(chan struct{}), allow: make(chan struct{})}
	p.client.AddHook(hook)
	hold := make(chan struct{})
	var releaseOnce, allowOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(hold) }) }
	allow := func() { allowOnce.Do(func() { close(hook.allow) }) }
	defer release()
	defer allow()
	for seq := 0; seq < 2; seq++ {
		for key := 0; key < 8; key++ {
			require.NoError(t, p.Publish(ctx, "gap", []byte(fmt.Sprintf("%d:%d", key, seq)), &PublishOptions{OrderingKey: fmt.Sprintf("key-%d", key)}))
		}
	}
	stop, err := p.SubscribeReliable(ctx, "gap", ReliableSubscribeOptions{Group: "workers", Consumer: "one", SharedKeys: true, KeyConcurrency: 8, Count: 32}, func(m *Message) error {
		var key, seq int
		if _, e := fmt.Sscanf(string(m.Data), "%d:%d", &key, &seq); e != nil {
			return e
		}
		if seq == 0 {
			started.Add(1)
			<-hold
		}
		return nil
	})
	require.NoError(t, err)
	defer stop()
	select {
	case <-hook.blocked:
	case <-time.After(3 * time.Second):
		t.Fatal("实际8个头尚未全部在途或未进入受控原PEL页")
	}
	release()
	require.Eventually(t, func() bool {
		return p.deliveryMetrics.ackSucceeded.Load() == 8 && p.deliveryMetrics.sharedLeases.Load() == 0
	}, 3*time.Second, time.Millisecond)
	allow()
	require.Eventually(t, func() bool {
		return p.deliveryMetrics.ackSucceeded.Load() == 16 && p.client.XPending(ctx, p.streamKey("gap"), "workers").Val().Count == 0
	}, 3*time.Second, time.Millisecond)
	stop()
	p.wg.Wait()
	hook.mu.Lock()
	defer hook.mu.Unlock()
	require.GreaterOrEqual(t, hook.total, 8, "必须实际观察到原PEL已知尾项的正文读取，不能零观察量通过")
	require.LessOrEqual(t, hook.max, 1, "同一原PEL页的候选正文逐条往返，阻挡已完成结果消费及后继派发")
	require.Greater(t, hook.batches, 0, "必须通过成熟pipeline合并实际正文命令")
	t.Logf("hint bodies=%d max_round_trips_per_page=%d multi_command_batches=%d", hook.total, hook.max, hook.batches)
}

// TestRedisSharedHintBatchErrorsDoNotChangePending验证原件缺失与取消均不删除或ACK同页其他有效消息。
func TestRedisSharedHintBatchErrorsDoNotChangePending(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	require.NoError(t, p.client.XGroupCreateMkStream(ctx, p.streamKey("errors"), "g", "0").Err())
	for i := 0; i < 2; i++ {
		require.NoError(t, p.Publish(ctx, "errors", []byte("synthetic"), &PublishOptions{OrderingKey: fmt.Sprint(i)}))
	}
	entries, err := p.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "g", Consumer: "original", Streams: []string{p.streamKey("errors"), ">"}, Count: 2}).Result()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	first, second := entries[0].Messages[0].ID, entries[0].Messages[1].ID
	rows, err := p.pendingBodies(ctx, p.streamKey("errors"), []string{first, "1-0", second})
	require.ErrorContains(t, err, "SHARED_PENDING_BODY_MISSING")
	require.Len(t, rows, 1)
	require.EqualValues(t, 2, p.client.XPending(ctx, p.streamKey("errors"), "g").Val().Count)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = p.pendingBodies(canceled, p.streamKey("errors"), []string{first, second})
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 2, p.client.XPending(ctx, p.streamKey("errors"), "g").Val().Count)
	rows, err = p.pendingBodies(ctx, p.streamKey("errors"), []string{first, second})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, first, rows[0].ID)
	require.Equal(t, second, rows[1].ID)
	require.EqualValues(t, 2, p.client.XPending(ctx, p.streamKey("errors"), "g").Val().Count, "正文读取不能代替成功后的fenced ACK")
}
