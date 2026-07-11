package cluster_test

import (
	"context"
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
