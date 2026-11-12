// 本文件复用Core既有barrierStore，经公开EventBridge验证健康满批持续时失败前序的重试公平性。
package event_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/require"
)

// retryTrafficStore使用Core自有barrierStore，持续补充有界健康原件；不是消费方私有夹具副本。
type retryTrafficStore struct {
	*barrierStore
	producing    atomic.Bool
	serial       uint
	loads        atomic.Int64
	markFailures atomic.Int64
}

func newRetryTrafficStore() *retryTrafficStore {
	s := &retryTrafficStore{barrierStore: &barrierStore{pending: []event.OutboxMessage{
		{ID: 1, EventID: "a1", EventType: "changed", Subject: "failed", ShardKey: "A"},
		{ID: 2, EventID: "a2", EventType: "changed", Subject: "failed", ShardKey: "A"},
	}}, serial: 2}
	s.producing.Store(true)
	return s
}
func (s *retryTrafficStore) load(ctx context.Context, limit int, skip []string) ([]event.OutboxMessage, error) {
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	s.mu.Lock()
	for s.producing.Load() && len(s.pending) < 2*limit+2 {
		s.serial++
		s.pending = append(s.pending, event.OutboxMessage{ID: s.serial, EventID: fmt.Sprint("b", s.serial), EventType: "changed", Subject: "healthy", ShardKey: "B"})
	}
	s.mu.Unlock()
	items, err := s.barrierStore.LoadPendingSkipping(ctx, limit, skip)
	if len(items) == limit {
		s.loads.Add(1)
	}
	return items, err
}

// LoadPending保持Core夹具的earliest-first，先补充健康数据但不改旧原件。
func (s *retryTrafficStore) LoadPending(ctx context.Context, n int) ([]event.OutboxMessage, error) {
	return s.load(ctx, n, nil)
}

// MarkPublished仅在上下文有效时调用原Core确认实现。
func (s *retryTrafficStore) MarkPublished(ctx context.Context, m event.OutboxMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.EventID == "a1" && s.markFailures.CompareAndSwap(1, 0) {
		return errors.New("Core合成确认事务失败")
	}
	return s.barrierStore.MarkPublished(ctx, m)
}

type retrySkippingStore struct{ *retryTrafficStore }

// LoadPendingSkipping沿用Core自有跳过算法。
func (s retrySkippingStore) LoadPendingSkipping(ctx context.Context, n int, skip []string) ([]event.OutboxMessage, error) {
	return s.load(ctx, n, skip)
}

type retryPublisherProbe struct {
	attempts       atomic.Int64
	healthy        atomic.Int64
	successors     atomic.Int64
	accepted       atomic.Bool
	violation      atomic.Bool
	first          chan struct{}
	permanent      atomic.Bool
	noPublishFault bool
}

// Publish只对第一个A原件注入首失败；后继必须在A前序成功之后才能进入。
func (p *retryPublisherProbe) Publish(_ context.Context, _ string, e *event.Envelope) error {
	switch e.ID {
	case "a1":
		n := p.attempts.Add(1)
		if n == 1 {
			close(p.first)
		}
		if n == 1 && !p.noPublishFault || p.permanent.Load() {
			return errors.New("Core合成临时发布失败")
		}
		p.accepted.Store(true)
	case "a2":
		p.successors.Add(1)
		if !p.accepted.Load() {
			p.violation.Store(true)
		}
	default:
		p.healthy.Add(1)
	}
	return nil
}

// TestOutboxRetryDuringContinuousDrain在健康生产仍开启时断言失败key恢复，不能先停生产使测试变绿。
func TestOutboxRetryDuringContinuousDrain(t *testing.T) {
	s := newRetryTrafficStore()
	external := &retryPublisherProbe{first: make(chan struct{})}
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{SubscriberID: "core-retry"})
	bridge.SetExternalPublisher(external)
	require.NoError(t, bridge.UseOutbox(event.OutboxOptions{SourceService: "core-retry", Store: retrySkippingStore{s}, Interval: 20 * time.Millisecond, BatchSize: 16, KeyConcurrency: 8, External: true}))
	t.Cleanup(func() {
		t.Logf("断言完成时健康生产=%t full_batches=%d healthy=%d first_attempts=%d successor=%d", s.producing.Load(), s.loads.Load(), external.healthy.Load(), external.attempts.Load(), external.successors.Load())
		s.producing.Store(false)
		require.NoError(t, bridge.Close(context.Background()))
	})
	bridge.NotifyOutbox()
	select {
	case <-external.first:
	case <-time.After(time.Second):
		t.Fatal("夹具未到首次失败")
	}
	require.Eventually(t, func() bool { return s.loads.Load() >= 12 && external.healthy.Load() >= 100 }, time.Second, time.Millisecond, "夹具必须确有持续健康满批进展")
	require.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		seen := map[string]bool{}
		for _, id := range s.marked {
			seen[id] = true
		}
		return seen["a1"] && seen["a2"]
	}, time.Second, time.Millisecond, "OUTBOX_RETRY_STARVATION: 健康生产持续时失败前序必须重新尝试并确认")
	require.True(t, s.producing.Load())
	require.False(t, external.violation.Load())
	t.Logf("持续健康生产仍开启：full_batches=%d healthy=%d first_attempts=%d successor=%d", s.loads.Load(), external.healthy.Load(), external.attempts.Load(), external.successors.Load())
}

// TestOutboxRetrySmallBatchDoesNotBlockHealthy固定BatchSize=1时，单个永久失败key不能用完技术屏障预算而阻断健康key。
func TestOutboxRetrySmallBatchDoesNotBlockHealthy(t *testing.T) {
	s := newRetryTrafficStore()
	probe := &retryPublisherProbe{first: make(chan struct{})}
	probe.permanent.Store(true)
	b := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{SubscriberID: "retry-small"})
	b.SetExternalPublisher(probe)
	require.NoError(t, b.UseOutbox(event.OutboxOptions{SourceService: "retry-small", Store: retrySkippingStore{s}, Interval: 20 * time.Millisecond, BatchSize: 1, External: true}))
	t.Cleanup(func() { s.producing.Store(false); require.NoError(t, b.Close(context.Background())) })
	b.NotifyOutbox()
	require.Eventually(t, func() bool { return probe.healthy.Load() >= 10 }, time.Second, time.Millisecond, "单个失败key不能在最小批次下饿死其他key")
	require.Zero(t, probe.successors.Load())
	require.True(t, s.producing.Load())
}
