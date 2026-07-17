package cluster_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRedisDiscoveryProvider(t *testing.T) *cluster.RedisProvider {
	t.Helper()
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("设置 CORE_TEST_REDIS_ADDR 后运行 Redis 服务发现集成测试")
	}
	prefix := fmt.Sprintf("core:test:discovery:%d", time.Now().UnixNano())
	provider, err := cluster.NewRedisProvider(addr, 0, prefix, 2*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, provider.Close()) })
	return provider
}

func TestRedisProvider_RegisterListHeartbeatAndDeregister(t *testing.T) {
	provider := newRedisDiscoveryProvider(t)
	ctx := context.Background()
	node := &cluster.NodeInfo{
		ID: "orders-1", ServiceName: "orders",
		DataCenterID: 1, MachineID: 1,
		Address: "order-1", Port: 8080, GRPCPort: 19090, Weight: 1,
	}

	require.NoError(t, provider.Register(ctx, node))
	nodes, err := provider.List(ctx, "orders", cluster.NodeStatusRunning)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "order-1", nodes[0].Address)
	assert.Equal(t, 19090, nodes[0].GRPCPort)

	require.NoError(t, provider.Heartbeat(ctx, node.ID))
	require.NoError(t, provider.Deregister(ctx, node.ID))
	nodes, err = provider.List(ctx, "orders", cluster.NodeStatusRunning)
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestRedisProvider_RejectsActiveMachineIDConflict(t *testing.T) {
	provider := newRedisDiscoveryProvider(t)
	ctx := context.Background()
	first := &cluster.NodeInfo{ID: "orders-a", ServiceName: "orders", DataCenterID: 1, MachineID: 7, Address: "a", Port: 8080}
	second := &cluster.NodeInfo{ID: "orders-b", ServiceName: "orders", DataCenterID: 1, MachineID: 7, Address: "b", Port: 8080}

	require.NoError(t, provider.Register(ctx, first))
	err := provider.Register(ctx, second)
	require.ErrorIs(t, err, cluster.ErrSlotConflict)
}

func TestRedisProvider_WatchReceivesRegistrationAndDeregistration(t *testing.T) {
	provider := newRedisDiscoveryProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updates := make(chan []*cluster.NodeInfo, 4)
	stop, err := provider.Watch(ctx, "orders", func(nodes []*cluster.NodeInfo) {
		updates <- nodes
	})
	require.NoError(t, err)
	defer stop()

	node := &cluster.NodeInfo{ID: "orders-watch", ServiceName: "orders", DataCenterID: 1, MachineID: 2, Address: "order", Port: 8080}
	require.NoError(t, provider.Register(ctx, node))
	require.Eventually(t, func() bool {
		select {
		case nodes := <-updates:
			return len(nodes) == 1 && nodes[0].ID == node.ID
		default:
			return false
		}
	}, 3*time.Second, 20*time.Millisecond)

	require.NoError(t, provider.Deregister(ctx, node.ID))
	require.Eventually(t, func() bool {
		select {
		case nodes := <-updates:
			return len(nodes) == 0
		default:
			return false
		}
	}, 3*time.Second, 20*time.Millisecond)
}
