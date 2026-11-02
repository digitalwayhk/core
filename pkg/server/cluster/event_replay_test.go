// 本文件验证恢复重放只用当前会话索引，重复心跳、同peer重启和退订竞争不复活订阅。
package cluster_test

import (
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCrossNodeReplayRecoversNewAndSameIdentityPeer(t *testing.T) {
	p := newBrokerProvider(t, "replay", "local")
	b := cluster.NewCrossNodeNoticeBroker(p, "replay", "local")
	b.OnSubscriptionChange("/route", 88, true)
	srv, _ := noticeCaptureSrv(t)
	registerPeerNode(t, p, "replay", "peer", srv)
	var calls, activeCalls atomic.Int64
	b.SetSender(func(_ context.Context, _ *cluster.NodeInfo, data []byte, _ string) ([]byte, error) {
		var row struct{ Active bool }
		_ = json.Unmarshal(data, &row)
		calls.Add(1)
		if row.Active {
			activeCalls.Add(1)
		}
		return nil, nil
	})
	require.NoError(t, b.ReplayLocalSubscriptions(context.Background()))
	require.EqualValues(t, 1, activeCalls.Load())
	// 同标识同地址重新建立内存索引，不依赖只见过一次ID的缓存。
	require.NoError(t, b.ReplayLocalSubscriptions(context.Background()))
	require.EqualValues(t, 2, activeCalls.Load())
	b.OnSubscriptionChange("/route", 88, false)
	require.Eventually(t, func() bool { return calls.Load() == 3 }, time.Second, time.Millisecond)
	require.NoError(t, b.ReplayLocalSubscriptions(context.Background()))
	require.EqualValues(t, 2, activeCalls.Load(), "退订后不得重放true")
}

func TestCrossNodeReplayUnsubscribeWaitsForBoundedSnapshot(t *testing.T) {
	p := newBrokerProvider(t, "replay-race", "local")
	b := cluster.NewCrossNodeNoticeBroker(p, "replay-race", "local")
	b.OnSubscriptionChange("/route", 88, true)
	srv, _ := noticeCaptureSrv(t)
	registerPeerNode(t, p, "replay-race", "peer", srv)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var flags []bool
	b.SetSender(func(ctx context.Context, _ *cluster.NodeInfo, data []byte, _ string) ([]byte, error) {
		var row struct{ Active bool }
		_ = json.Unmarshal(data, &row)
		if row.Active {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		mu.Lock()
		flags = append(flags, row.Active)
		mu.Unlock()
		return nil, nil
	})
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { done <- b.ReplayLocalSubscriptions(ctx) }()
	<-entered
	unsubscribed := make(chan struct{})
	go func() { b.OnSubscriptionChange("/route", 88, false); close(unsubscribed) }()
	close(release)
	require.NoError(t, <-done)
	<-unsubscribed
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(flags) == 2 }, time.Second, time.Millisecond)
	require.NoError(t, b.ReplayLocalSubscriptions(ctx))
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []bool{true, false}, flags)
}

func TestCrossNodeReplayWorkIsBoundedAndCanceled(t *testing.T) {
	p := newBrokerProvider(t, "replay-bounded", "local")
	b := cluster.NewCrossNodeNoticeBroker(p, "replay-bounded", "local")
	for i := uint64(1); i <= 200; i++ {
		b.OnSubscriptionChange("/route", i, true)
	}
	srv, _ := noticeCaptureSrv(t)
	registerPeerNode(t, p, "replay-bounded", "peer", srv)
	var calls atomic.Int64
	b.SetSender(func(context.Context, *cluster.NodeInfo, []byte, string) ([]byte, error) {
		calls.Add(1)
		return nil, nil
	})
	require.NoError(t, b.ReplayLocalSubscriptions(context.Background()))
	require.EqualValues(t, 128, calls.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, b.ReplayLocalSubscriptions(ctx), context.Canceled)
}
