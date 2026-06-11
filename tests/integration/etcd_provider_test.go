//go:build integration

package integration_test

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

// etcdEndpoints returns the etcd endpoints from env, defaulting to localhost.
func etcdEndpoints() []string {
	ep := os.Getenv("ETCD_ENDPOINTS")
	if ep == "" {
		return []string{"localhost:2379"}
	}
	return []string{ep}
}

// TestClusterEtcd_RegisterAndList verifies basic register+list round-trip.
func TestClusterEtcd_RegisterAndList(t *testing.T) {
	if os.Getenv("CORE_TEST_ETCD") == "" {
		t.Skip("CORE_TEST_ETCD not set")
	}
	p, err := cluster.NewEtcdProvider(etcdEndpoints(), 0)
	require.NoError(t, err)
	defer p.Close()

	ctx := context.Background()
	n := &cluster.NodeInfo{
		ID:           "test-etcd-register-node",
		ServiceName:  "test-etcd-svc",
		DataCenterID: 1,
		MachineID:    5,
		Address:      "127.0.0.1",
		Port:         9001,
		Weight:       1,
	}
	require.NoError(t, p.Register(ctx, n))
	defer p.Deregister(ctx, n.ID)

	nodes, err := p.List(ctx, "test-etcd-svc")
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, n.ID, nodes[0].ID)
	assert.Equal(t, cluster.NodeStatusRunning, nodes[0].Status)
}

// TestClusterEtcd_SlotConflict checks that registering the same DC+Machine slot
// for a different node ID returns ErrSlotConflict.
func TestClusterEtcd_SlotConflict(t *testing.T) {
	if os.Getenv("CORE_TEST_ETCD") == "" {
		t.Skip("CORE_TEST_ETCD not set")
	}
	p, err := cluster.NewEtcdProvider(etcdEndpoints(), 0)
	require.NoError(t, err)
	defer p.Close()

	ctx := context.Background()
	n1 := &cluster.NodeInfo{
		ID:           "test-etcd-conflict-n1",
		ServiceName:  "test-etcd-conflict-svc",
		DataCenterID: 0,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         9002,
		Weight:       1,
	}
	require.NoError(t, p.Register(ctx, n1))
	defer p.Deregister(ctx, n1.ID)

	n2 := &cluster.NodeInfo{
		ID:           "test-etcd-conflict-n2",
		ServiceName:  "test-etcd-conflict-svc",
		DataCenterID: 0,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         9003,
		Weight:       1,
	}
	err = p.Register(ctx, n2)
	assert.ErrorIs(t, err, cluster.ErrSlotConflict)
}

// TestClusterEtcd_Heartbeat_KeepsAlive verifies that Heartbeat does not error
// for a registered node.
func TestClusterEtcd_Heartbeat_KeepsAlive(t *testing.T) {
	if os.Getenv("CORE_TEST_ETCD") == "" {
		t.Skip("CORE_TEST_ETCD not set")
	}
	p, err := cluster.NewEtcdProvider(etcdEndpoints(), 0)
	require.NoError(t, err)
	defer p.Close()

	ctx := context.Background()
	n := &cluster.NodeInfo{
		ID:           "test-etcd-heartbeat-node",
		ServiceName:  "test-etcd-hb-svc",
		DataCenterID: 0,
		MachineID:    2,
		Address:      "127.0.0.1",
		Port:         9004,
		Weight:       1,
	}
	require.NoError(t, p.Register(ctx, n))
	defer p.Deregister(ctx, n.ID)

	require.NoError(t, p.Heartbeat(ctx, n.ID))
}

// TestClusterEtcd_Watch fires the callback when a node is registered.
func TestClusterEtcd_Watch(t *testing.T) {
	if os.Getenv("CORE_TEST_ETCD") == "" {
		t.Skip("CORE_TEST_ETCD not set")
	}
	p, err := cluster.NewEtcdProvider(etcdEndpoints(), 0)
	require.NoError(t, err)
	defer p.Close()

	ctx := context.Background()
	fired := make(chan []*cluster.NodeInfo, 1)
	cancel, err := p.Watch(ctx, "test-etcd-watch-svc", func(nodes []*cluster.NodeInfo) {
		select {
		case fired <- nodes:
		default:
		}
	})
	require.NoError(t, err)
	defer cancel()

	n := &cluster.NodeInfo{
		ID:           "test-etcd-watch-node",
		ServiceName:  "test-etcd-watch-svc",
		DataCenterID: 0,
		MachineID:    3,
		Address:      "127.0.0.1",
		Port:         9005,
		Weight:       1,
	}
	require.NoError(t, p.Register(ctx, n))
	defer p.Deregister(ctx, n.ID)

	select {
	case nodes := <-fired:
		assert.NotEmpty(t, nodes)
	case <-time.After(5 * time.Second):
		t.Fatal("Watch callback not fired within 5 s")
	}
}

// TestClusterEtcd_AllocateMachineID allocates the lowest free slot.
func TestClusterEtcd_AllocateMachineID(t *testing.T) {
	if os.Getenv("CORE_TEST_ETCD") == "" {
		t.Skip("CORE_TEST_ETCD not set")
	}
	p, err := cluster.NewEtcdProvider(etcdEndpoints(), 0)
	require.NoError(t, err)
	defer p.Close()

	ctx := context.Background()
	svc := "test-etcd-alloc-svc"

	// Register machines 0 and 1; machine 2 should be free.
	for i := int64(0); i < 2; i++ {
		n := &cluster.NodeInfo{
			ID:           fmt.Sprintf("%s-n%d", svc, i),
			ServiceName:  svc,
			DataCenterID: 0,
			MachineID:    i,
			Address:      "127.0.0.1",
			Port:         int(9010 + i),
			Weight:       1,
		}
		require.NoError(t, p.Register(ctx, n))
		defer p.Deregister(ctx, n.ID)
	}

	id, err := p.AllocateMachineID(ctx, svc, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), id)
}

// TestClusterEtcd_ProviderSwitcher tests Begin → Complete migration.
func TestClusterEtcd_ProviderSwitcher(t *testing.T) {
	if os.Getenv("CORE_TEST_ETCD") == "" {
		t.Skip("CORE_TEST_ETCD not set")
	}
	// Start with local provider.
	local := cluster.NewLocalProvider(2*time.Second, 2*time.Second, 500*time.Millisecond)
	local.Start()
	defer local.Close()

	ctx := context.Background()
	// Register a node in the local provider.
	n := &cluster.NodeInfo{
		ID:           "switcher-local-node",
		ServiceName:  "switcher-svc",
		DataCenterID: 0,
		MachineID:    0,
		Address:      "127.0.0.1",
		Port:         9020,
		Weight:       1,
	}
	require.NoError(t, local.Register(ctx, n))

	switcher := cluster.NewClusterSwitcher(local, "switcher-svc")

	// Begin migration to etcd.
	etcdP, err := cluster.NewEtcdProvider(etcdEndpoints(), 0)
	require.NoError(t, err)
	defer etcdP.Close()

	require.NoError(t, switcher.Begin(ctx, etcdP))
	require.NoError(t, switcher.Complete(ctx))

	// After completion, Current() should be the etcd provider.
	assert.Equal(t, "etcd", switcher.Current().Name())
}
