// 本文件用确定性时钟验证发布器窗口与Notify资格，及实际drain中的取消和屏障内存界限。
package event

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestOutboxRetryWindowState固定到期前/到期时和每窗口一次Notify提前重试，不靠毫秒SLO。
func TestOutboxRetryWindowState(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &outboxPublisher{blocked: map[string]struct{}{"A": {}}, retryAt: now.Add(time.Minute)}
	p.advanceOutboxRetry(now, false)
	require.Len(t, p.blocked, 1)
	p.advanceOutboxRetry(now, true)
	require.Empty(t, p.blocked)
	require.True(t, p.retryExpedited)
	p.blocked["A"] = struct{}{}
	p.advanceOutboxRetry(now.Add(time.Second), true)
	require.Len(t, p.blocked, 1)
	p.advanceOutboxRetry(now.Add(time.Minute-time.Nanosecond), false)
	require.Len(t, p.blocked, 1)
	p.advanceOutboxRetry(now.Add(time.Minute), false)
	require.Empty(t, p.blocked)
	require.False(t, p.retryExpedited)
	require.True(t, p.retryAt.IsZero())
}

type retryBoundStore struct {
	items []OutboxMessage
	loads int
}

// LoadPending沿用有界夹具的earliest-first取批。
func (s *retryBoundStore) LoadPending(ctx context.Context, n int) ([]OutboxMessage, error) {
	return s.LoadPendingSkipping(ctx, n, nil)
}

// LoadPendingSkipping追加至多一批原件并保留所有失败项。
func (s *retryBoundStore) LoadPendingSkipping(_ context.Context, n int, skip []string) ([]OutboxMessage, error) {
	s.loads++
	for range n {
		id := fmt.Sprint(len(s.items))
		s.items = append(s.items, OutboxMessage{EventID: id, EventType: "changed", ShardKey: id})
	}
	blocked := map[string]bool{}
	for _, key := range skip {
		blocked[key] = true
	}
	out := []OutboxMessage{}
	for _, item := range s.items {
		if !blocked[item.ShardKey] {
			out = append(out, item)
		}
		if len(out) == n {
			break
		}
	}
	return out, nil
}

// MarkPublished拒绝把合成永久失败当成已确认。
func (*retryBoundStore) MarkPublished(context.Context, OutboxMessage) error {
	return errors.New("失败原件不应确认")
}

type retryAlwaysFails struct{}

// Publish固定失败以验证屏障容量与取消。
func (retryAlwaysFails) Publish(context.Context, string, *Envelope) error {
	return errors.New("Core合成永久故障")
}

// TestOutboxRetryBlockedBudgetKeepsOriginals多批不同失败key不能无限增长内存，原件继续保存在store。
func TestOutboxRetryBlockedBudgetKeepsOriginals(t *testing.T) {
	b := NewServiceEventBridge(NewStream(), ServiceEventBridgeOptions{SubscriberID: "bound"})
	b.SetExternalPublisher(retryAlwaysFails{})
	defer b.Close(context.Background())
	s := &retryBoundStore{}
	p := &outboxPublisher{source: "bound", bridge: b, store: s, external: true, batch: 3, keyConcurrency: 2, interval: time.Hour}
	p.drain(context.Background())
	require.Equal(t, 2, s.loads)
	require.Len(t, p.blocked, 6)
	require.Len(t, s.items, 6)
	require.LessOrEqual(t, len(p.blocked), 3*p.batch-1)
	require.EqualValues(t, 0, p.workerInflight.Load())
	require.LessOrEqual(t, p.workerPeak.Load(), int64(2))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.drain(ctx)
	require.Equal(t, 2, s.loads, "取消后不得进入新的Load")
}
