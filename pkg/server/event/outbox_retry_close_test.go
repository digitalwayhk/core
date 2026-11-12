// 本文件验证重试窗口中的取消/关闭仍等待实际发布回调退出，不确认取消结果或越过同key前序。
package event_test

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/require"
	"sync/atomic"
	"testing"
	"time"
)

type retryClosingPublisher struct {
	entered  chan struct{}
	canceled chan struct{}
	release  chan struct{}
	calls    atomic.Int64
}

// Publish取消后仍等待显式释放，用于验证真实回调退出边界。
func (p *retryClosingPublisher) Publish(ctx context.Context, _ string, _ *event.Envelope) error {
	p.calls.Add(1)
	close(p.entered)
	<-ctx.Done()
	close(p.canceled)
	<-p.release
	return ctx.Err()
}

// TestOutboxRetryCloseWaitsForActualPublisher以回调屏障证明取消不等于实际退出，失败原件与后继均保留。
func TestOutboxRetryCloseWaitsForActualPublisher(t *testing.T) {
	s := newRetryTrafficStore()
	s.producing.Store(false)
	p := &retryClosingPublisher{entered: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
	b := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{SubscriberID: "retry-close"})
	b.SetExternalPublisher(p)
	require.NoError(t, b.UseOutbox(event.OutboxOptions{SourceService: "retry-close", Store: retrySkippingStore{s}, Interval: 20 * time.Millisecond, BatchSize: 2, KeyConcurrency: 2, External: true}))
	released := false
	defer func() {
		if !released {
			close(p.release)
		}
		require.NoError(t, b.Close(context.Background()))
	}()
	b.NotifyOutbox()
	select {
	case <-p.entered:
	case <-time.After(time.Second):
		t.Fatal("发布回调未进入")
	}
	done := make(chan error, 1)
	go func() { done <- b.Close(context.Background()) }()
	select {
	case <-p.canceled:
	case <-time.After(time.Second):
		t.Fatal("取消未到达实际回调")
	}
	select {
	case err := <-done:
		t.Fatalf("实际回调仍阻塞，Close不得提前完成: %v", err)
	default:
	}
	s.mu.Lock()
	pending, marked := len(s.pending), len(s.marked)
	s.mu.Unlock()
	require.Equal(t, 2, pending)
	require.Zero(t, marked)
	require.EqualValues(t, 1, p.calls.Load())
	close(p.release)
	released = true
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("实际回调退出后Close未收敛")
	}
	require.EqualValues(t, 1, p.calls.Load())
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Len(t, s.pending, 2)
	require.Empty(t, s.marked)
}
