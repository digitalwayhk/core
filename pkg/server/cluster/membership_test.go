package cluster_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type membershipRegistry struct {
	heartbeat  func(context.Context, string) error
	deregister func(context.Context, string) error
}

func TestMembershipManagerStopRetriesAndPreservesError(t *testing.T) {
	wantErr := errors.New("redis unavailable")
	var calls atomic.Int32
	registry := &membershipRegistry{deregister: func(context.Context, string) error {
		calls.Add(1)
		return wantErr
	}}
	manager := cluster.NewMembershipManager(registry, "node-retry", time.Hour,
		cluster.WithDeregisterRetry(3, 0))
	manager.Start(context.Background())

	first := manager.Stop(context.Background())
	second := manager.Stop(context.Background())

	require.ErrorIs(t, first, wantErr)
	require.ErrorIs(t, second, wantErr)
	assert.Equal(t, int32(3), calls.Load())
}

func TestMembershipManagerStopHonorsContext(t *testing.T) {
	entered := make(chan struct{})
	registry := &membershipRegistry{deregister: func(ctx context.Context, _ string) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}}
	manager := cluster.NewMembershipManager(registry, "node-timeout", time.Hour,
		cluster.WithDeregisterRetry(3, 0), cluster.WithDeregisterTimeout(50*time.Millisecond))
	manager.Start(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- manager.Stop(ctx) }()
	<-entered
	cancel()

	require.ErrorIs(t, <-result, context.Canceled)
	require.Eventually(t, func() bool {
		return errors.Is(manager.Stop(context.Background()), context.DeadlineExceeded)
	}, time.Second, 10*time.Millisecond)
}

func TestMembershipManagerConcurrentStopsShareFailure(t *testing.T) {
	wantErr := errors.New("deregister unavailable")
	var calls atomic.Int32
	registry := &membershipRegistry{deregister: func(context.Context, string) error {
		calls.Add(1)
		return wantErr
	}}
	manager := cluster.NewMembershipManager(registry, "node-concurrent-error", time.Hour,
		cluster.WithDeregisterRetry(3, 0))
	manager.Start(context.Background())

	results := make(chan error, 16)
	var stopped sync.WaitGroup
	for range 16 {
		stopped.Add(1)
		go func() {
			defer stopped.Done()
			results <- manager.Stop(context.Background())
		}()
	}
	stopped.Wait()
	close(results)
	for err := range results {
		require.ErrorIs(t, err, wantErr)
	}
	assert.Equal(t, int32(3), calls.Load())
}

func TestMembershipManagerStopDeadlineDoesNotPoisonSharedResult(t *testing.T) {
	heartbeatEntered := make(chan struct{})
	releaseHeartbeat := make(chan struct{})
	deregisterEntered := make(chan struct{})
	releaseDeregister := make(chan struct{})
	wantErr := errors.New("final deregister failure")
	var heartbeatOnce sync.Once
	var deregisterOnce sync.Once
	registry := &membershipRegistry{
		heartbeat: func(context.Context, string) error {
			heartbeatOnce.Do(func() { close(heartbeatEntered) })
			<-releaseHeartbeat
			return nil
		},
		deregister: func(context.Context, string) error {
			deregisterOnce.Do(func() { close(deregisterEntered) })
			<-releaseDeregister
			return wantErr
		},
	}
	manager := cluster.NewMembershipManager(registry, "node-deadlines", time.Nanosecond,
		cluster.WithDeregisterRetry(1, 0))
	manager.Start(context.Background())
	<-heartbeatEntered

	shortCtx, cancel := context.WithCancel(context.Background())
	shortResult := make(chan error, 1)
	go func() { shortResult <- manager.Stop(shortCtx) }()
	cancel()
	require.ErrorIs(t, <-shortResult, context.Canceled)

	close(releaseHeartbeat)
	<-deregisterEntered
	longResult := make(chan error, 1)
	go func() { longResult <- manager.Stop(context.Background()) }()
	close(releaseDeregister)
	require.ErrorIs(t, <-longResult, wantErr)
	require.ErrorIs(t, manager.Stop(context.Background()), wantErr)
}

func (r *membershipRegistry) Register(context.Context, *cluster.NodeInfo) error { return nil }

func (r *membershipRegistry) Deregister(ctx context.Context, nodeID string) error {
	if r.deregister != nil {
		return r.deregister(ctx, nodeID)
	}
	return nil
}

func (r *membershipRegistry) Heartbeat(ctx context.Context, nodeID string) error {
	if r.heartbeat != nil {
		return r.heartbeat(ctx, nodeID)
	}
	return nil
}

func (r *membershipRegistry) Get(context.Context, string) (*cluster.NodeInfo, error) {
	return nil, cluster.ErrNodeNotFound
}

func (r *membershipRegistry) List(context.Context, string, ...cluster.NodeStatus) ([]*cluster.NodeInfo, error) {
	return nil, nil
}

func (r *membershipRegistry) Watch(context.Context, string, func([]*cluster.NodeInfo)) (func(), error) {
	return func() {}, nil
}

func (r *membershipRegistry) Close() error { return nil }

func TestMembershipManager_ConcurrentStartOnlyRunsOneWorker(t *testing.T) {
	heartbeatStarted := make(chan struct{})
	releaseHeartbeat := make(chan struct{})
	var heartbeatCount atomic.Int32
	var startedOnce sync.Once
	registry := &membershipRegistry{
		heartbeat: func(context.Context, string) error {
			heartbeatCount.Add(1)
			startedOnce.Do(func() { close(heartbeatStarted) })
			<-releaseHeartbeat
			return nil
		},
	}
	manager := cluster.NewMembershipManager(registry, "node-1", time.Millisecond)

	var starts sync.WaitGroup
	for range 16 {
		starts.Add(1)
		go func() {
			defer starts.Done()
			manager.Start(context.Background())
		}()
	}
	starts.Wait()

	select {
	case <-heartbeatStarted:
	case <-time.After(time.Second):
		t.Fatal("未等到心跳 worker 启动")
	}
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, int32(1), heartbeatCount.Load(), "并发 Start 不应创建多个 worker")

	close(releaseHeartbeat)
	manager.Stop(context.Background())
}

func TestMembershipManager_ConcurrentStopDeregistersOnce(t *testing.T) {
	var deregisterCount atomic.Int32
	registry := &membershipRegistry{
		deregister: func(context.Context, string) error {
			deregisterCount.Add(1)
			return nil
		},
	}
	manager := cluster.NewMembershipManager(registry, "node-1", time.Hour)
	manager.Start(context.Background())

	var stops sync.WaitGroup
	for range 16 {
		stops.Add(1)
		go func() {
			defer stops.Done()
			manager.Stop(context.Background())
		}()
	}
	stops.Wait()

	assert.Equal(t, int32(1), deregisterCount.Load(), "重复 Stop 只能注销一次")
}

func TestMembershipManager_StopWaitsForWorker(t *testing.T) {
	heartbeatStarted := make(chan struct{})
	releaseHeartbeat := make(chan struct{})
	var heartbeatStartedOnce sync.Once
	registry := &membershipRegistry{
		heartbeat: func(context.Context, string) error {
			heartbeatStartedOnce.Do(func() { close(heartbeatStarted) })
			<-releaseHeartbeat
			return nil
		},
	}
	manager := cluster.NewMembershipManager(registry, "node-1", time.Millisecond)
	manager.Start(context.Background())

	select {
	case <-heartbeatStarted:
	case <-time.After(time.Second):
		t.Fatal("未等到心跳开始")
	}

	stopped := make(chan struct{})
	go func() {
		manager.Stop(context.Background())
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Fatal("Stop 在 worker 退出前提前返回")
	case <-time.After(30 * time.Millisecond):
	}

	close(releaseHeartbeat)
	require.Eventually(t, func() bool {
		select {
		case <-stopped:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
}

func TestMembershipManager_StopIsBoundedWhenHeartbeatIgnoresContext(t *testing.T) {
	heartbeatStarted := make(chan struct{})
	blockForever := make(chan struct{})
	var heartbeatOnce sync.Once
	var deregisterCount atomic.Int32
	registry := &membershipRegistry{
		heartbeat: func(context.Context, string) error {
			heartbeatOnce.Do(func() { close(heartbeatStarted) })
			<-blockForever
			return nil
		},
		deregister: func(context.Context, string) error {
			deregisterCount.Add(1)
			return nil
		},
	}
	manager := cluster.NewMembershipManager(registry, "node-stuck-heartbeat", time.Nanosecond,
		cluster.WithDeregisterRetry(1, 0),
		cluster.WithDeregisterTimeout(80*time.Millisecond))
	manager.Start(context.Background())
	<-heartbeatStarted

	startedAt := time.Now()
	results := make(chan error, 8)
	var waiters sync.WaitGroup
	for range 8 {
		waiters.Add(1)
		go func() {
			defer waiters.Done()
			results <- manager.Stop(context.Background())
		}()
	}
	waiters.Wait()
	close(results)

	assert.Less(t, time.Since(startedAt), 300*time.Millisecond, "Membership Stop 必须受共享总上限约束")
	assert.Equal(t, int32(1), deregisterCount.Load(), "心跳 worker 卡住时仍必须尝试注销一次")
	var first error
	for err := range results {
		require.Error(t, err, "心跳 worker 未退出必须记录终态错误")
		if first == nil {
			first = err
			continue
		}
		assert.Equal(t, first.Error(), err.Error(), "所有 Stop 等待者必须观察同一共享注销结果")
	}

	close(blockForever)
}
