// 本文件验证Outbox各旧能力组合、标记失败和通知背压；复用Core自有持续流夹具。
package event_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/require"
)

type retryPlainStore struct{ s *retryTrafficStore }

// LoadPending只暴露旧必选接口，不意外继承SkipBlocked能力。
func (s retryPlainStore) LoadPending(c context.Context, n int) ([]event.OutboxMessage, error) {
	return s.s.LoadPending(c, n)
}

// MarkPublished沿用Core夹具的实际确认。
func (s retryPlainStore) MarkPublished(c context.Context, m event.OutboxMessage) error {
	return s.s.MarkPublished(c, m)
}

type retryBatchStore struct {
	store event.OutboxStore
	s     *retryTrafficStore
}

// LoadPending保留底层earliest-first结果。
func (s *retryBatchStore) LoadPending(c context.Context, n int) ([]event.OutboxMessage, error) {
	return s.store.LoadPending(c, n)
}

// MarkPublished保持逐条接口兼容。
func (s *retryBatchStore) MarkPublished(c context.Context, m event.OutboxMessage) error {
	return s.store.MarkPublished(c, m)
}

// MarkPublishedBatch以单个Core夹具互斥验证全成/全败，不借逐条确认伪装批量事务。
func (s *retryBatchStore) MarkPublishedBatch(c context.Context, messages []event.OutboxMessage) error {
	if len(messages) == 0 {
		return nil
	}
	if e := c.Err(); e != nil {
		return e
	}
	s.s.mu.Lock()
	defer s.s.mu.Unlock()
	selected := map[string]bool{}
	known := map[string]bool{}
	for _, m := range s.s.pending {
		known[m.EventID] = true
	}
	for _, id := range s.s.marked {
		known[id] = true
	}
	for _, m := range messages {
		if m.ID == 0 || !known[m.EventID] {
			return errors.New("Core合成未知确认原件")
		}
		selected[m.EventID] = true
	}
	if selected["a1"] && s.s.markFailures.CompareAndSwap(1, 0) {
		return errors.New("Core合成批确认事务失败")
	}
	for id := range selected {
		s.s.marked = append(s.s.marked, id)
	}
	keep := s.s.pending[:0]
	for _, m := range s.s.pending {
		if !selected[m.EventID] {
			keep = append(keep, m)
		}
	}
	s.s.pending = keep
	return nil
}

type retrySkipBatchStore struct{ *retryBatchStore }

// LoadPendingSkipping显式启用跳过能力，过滤后仍按原件先后返回。
func (s retrySkipBatchStore) LoadPendingSkipping(c context.Context, n int, skip []string) ([]event.OutboxMessage, error) {
	return s.s.load(c, n, skip)
}

func retryStoreCapabilities(s *retryTrafficStore, skip, batch bool) event.OutboxStore {
	if batch {
		b := &retryBatchStore{store: retryPlainStore{s}, s: s}
		if skip {
			return retrySkipBatchStore{b}
		}
		return b
	}
	if skip {
		return retrySkippingStore{s}
	}
	return retryPlainStore{s}
}
func retryConfirmedPair(s *retryTrafficStore) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	for _, id := range s.marked {
		seen[id] = true
	}
	return seen["a1"] && seen["a2"]
}

// TestOutboxRetryCapabilityMatrix覆盖默认/串行/分key并行、SkipBlocked和BatchMarker有无、发布/确认失败。
func TestOutboxRetryCapabilityMatrix(t *testing.T) {
	for _, n := range []int{0, 1, 8} {
		for _, skip := range []bool{false, true} {
			for _, batch := range []bool{false, true} {
				for _, markFailure := range []bool{false, true} {
					t.Run(fmt.Sprintf("workers=%d/skip=%t/batch=%t/mark=%t", n, skip, batch, markFailure), func(t *testing.T) {
						s := newRetryTrafficStore()
						if markFailure {
							s.markFailures.Store(1)
						}
						probe := &retryPublisherProbe{first: make(chan struct{}), noPublishFault: markFailure}
						store := retryStoreCapabilities(s, skip, batch)
						_, hasSkip := store.(event.OutboxStoreSkipBlocked)
						_, hasBatch := store.(event.OutboxBatchMarker)
						require.Equal(t, skip, hasSkip)
						require.Equal(t, batch, hasBatch)
						b := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{SubscriberID: "retry-matrix"})
						b.SetExternalPublisher(probe)
						require.NoError(t, b.UseOutbox(event.OutboxOptions{SourceService: "retry-matrix", Store: store, Interval: 20 * time.Millisecond, BatchSize: 16, KeyConcurrency: n, External: true}))
						t.Cleanup(func() { s.producing.Store(false); require.NoError(t, b.Close(context.Background())) })
						b.NotifyOutbox()
						require.Eventually(t, func() bool { return s.loads.Load() >= 12 && retryConfirmedPair(s) }, 2*time.Second, time.Millisecond)
						require.True(t, s.producing.Load())
						require.False(t, probe.violation.Load())
						require.GreaterOrEqual(t, probe.attempts.Load(), int64(2))
					})
				}
			}
		}
	}
}

// TestOutboxRetryNotifyCannotSpinPermanentFailure以实际经过时间核对窗口重试上界，通知不无限提前清屏障。
func TestOutboxRetryNotifyCannotSpinPermanentFailure(t *testing.T) {
	for _, healthy := range []bool{false, true} {
		t.Run(fmt.Sprint(healthy), func(t *testing.T) {
			s := newRetryTrafficStore()
			s.producing.Store(healthy)
			p := &retryPublisherProbe{first: make(chan struct{})}
			p.permanent.Store(true)
			b := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{SubscriberID: "retry-budget"})
			b.SetExternalPublisher(p)
			interval := 40 * time.Millisecond
			require.NoError(t, b.UseOutbox(event.OutboxOptions{SourceService: "retry-budget", Store: retrySkippingStore{s}, Interval: interval, BatchSize: 16, KeyConcurrency: 8, External: true}))
			t.Cleanup(func() { s.producing.Store(false); require.NoError(t, b.Close(context.Background())) })
			started := time.Now()
			b.NotifyOutbox()
			select {
			case <-p.first:
			case <-time.After(time.Second):
				t.Fatal("首次失败未进入")
			}
			for range 120 {
				b.NotifyOutbox()
				time.Sleep(time.Millisecond)
			}
			elapsed := time.Since(started)
			attempts := p.attempts.Load()
			limit := 2 * (int64(elapsed/interval) + 2)
			require.GreaterOrEqual(t, attempts, int64(2))
			require.LessOrEqual(t, attempts, limit, "永久失败不能因Notify风暴忙重试")
			require.Zero(t, p.successors.Load())
			require.False(t, retryConfirmedPair(s))
			if healthy {
				require.Positive(t, p.healthy.Load())
			}
			t.Logf("healthy=%t notify=120 attempts=%d upper=%d elapsed=%s", healthy, attempts, limit, elapsed)
			s.producing.Store(false)
			require.NoError(t, b.Close(context.Background()))
			closed := p.attempts.Load()
			for range 100 {
				b.NotifyOutbox()
			}
			time.Sleep(2 * interval)
			require.Equal(t, closed, p.attempts.Load(), "关闭后不遗留timer/发布工作")
		})
	}
}
