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

// consulAddress returns the Consul HTTP address from env, defaulting to localhost.
func consulAddress() string {
	addr := os.Getenv("CONSUL_HTTP_ADDR")
	if addr == "" {
		return "127.0.0.1:8500"
	}
	return addr
}

// TestClusterConsul_RegisterAndList verifies basic register+list round-trip.
func TestClusterConsul_RegisterAndList(t *testing.T) {
	if os.Getenv("CORE_TEST_CONSUL") == "" {
		t.Skip("CORE_TEST_CONSUL not set")
	}
	p, err := cluster.NewConsulProvider(consulAddress())
	require.NoError(t, err)
	defer p.Close()

	ctx := context.Background()
	n := &cluster.NodeInfo{
		ID:           "test-consul-register-node",
		ServiceName:  "test-consul-svc",
		DataCenterID: 1,
		MachineID:    5,
		Address:      "127.0.0.1",
		Port:         9101,
		Weight:       1,
	}
	require.NoError(t, p.Register(ctx, n))
	defer p.Deregister(ctx, n.ID)

	nodes, err := p.List(ctx, "test-consul-svc")
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, n.ID, nodes[0].ID)
}

// TestClusterConsul_SlotConflict checks that registering the same DC+Machine slot
// for a different node ID returns ErrSlotConflict.
func TestClusterConsul_SlotConflict(t *testing.T) {
	if os.Getenv("CORE_TEST_CONSUL") == "" {
		t.Skip("CORE_TEST_CONSUL not set")
	}
	p, err := cluster.NewConsulProvider(consulAddress())
	require.NoError(t, err)
	defer p.Close()

	ctx := context.Background()
	n1 := &cluster.NodeInfo{
		ID:           "test-consul-conflict-n1",
		ServiceName:  "test-consul-conflict-svc",
		DataCenterID: 0,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         9102,
		Weight:       1,
	}
	require.NoError(t, p.Register(ctx, n1))
	defer p.Deregister(ctx, n1.ID)

	n2 := &cluster.NodeInfo{
		ID:           "test-consul-conflict-n2",
		ServiceName:  "test-consul-conflict-svc",
		DataCenterID: 0,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         9103,
		Weight:       1,
	}
	err = p.Register(ctx, n2)
	assert.ErrorIs(t, err, cluster.ErrSlotConflict)
}

// TestClusterConsul_Heartbeat verifies that passing a TTL health check works.
func TestClusterConsul_Heartbeat(t *testing.T) {
	if os.Getenv("CORE_TEST_CONSUL") == "" {
		t.Skip("CORE_TEST_CONSUL not set")
	}
	p, err := cluster.NewConsulProvider(consulAddress())
	require.NoError(t, err)
	defer p.Close()

	ctx := context.Background()
	n := &cluster.NodeInfo{
		ID:           "test-consul-heartbeat-node",
		ServiceName:  "test-consul-hb-svc",
		DataCenterID: 0,
		MachineID:    2,
		Address:      "127.0.0.1",
		Port:         9104,
		Weight:       1,
	}
	require.NoError(t, p.Register(ctx, n))
	defer p.Deregister(ctx, n.ID)

	require.NoError(t, p.Heartbeat(ctx, n.ID))
}

// TestClusterConsul_AllocateMachineID allocates the lowest free slot.
func TestClusterConsul_AllocateMachineID(t *testing.T) {
	if os.Getenv("CORE_TEST_CONSUL") == "" {
		t.Skip("CORE_TEST_CONSUL not set")
	}
	p, err := cluster.NewConsulProvider(consulAddress())
	require.NoError(t, err)
	defer p.Close()

	ctx := context.Background()
	svc := "test-consul-alloc-svc"

	// Register machines 0 and 1; machine 2 should be free.
	for i := int64(0); i < 2; i++ {
		n := &cluster.NodeInfo{
			ID:           fmt.Sprintf("%s-n%d", svc, i),
			ServiceName:  svc,
			DataCenterID: 0,
			MachineID:    i,
			Address:      "127.0.0.1",
			Port:         int(9110 + i),
			Weight:       1,
		}
		require.NoError(t, p.Register(ctx, n))
		defer p.Deregister(ctx, n.ID)
	}

	id, err := p.AllocateMachineID(ctx, svc, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), id)
}

// TestClusterConsul_Deregister marks a node offline.
func TestClusterConsul_Deregister(t *testing.T) {
	if os.Getenv("CORE_TEST_CONSUL") == "" {
		t.Skip("CORE_TEST_CONSUL not set")
	}
	p, err := cluster.NewConsulProvider(consulAddress())
	require.NoError(t, err)
	defer p.Close()

	ctx := context.Background()
	n := &cluster.NodeInfo{
		ID:           "test-consul-deregister-node",
		ServiceName:  "test-consul-dereg-svc",
		DataCenterID: 0,
		MachineID:    3,
		Address:      "127.0.0.1",
		Port:         9120,
		Weight:       1,
	}
	require.NoError(t, p.Register(ctx, n))

	require.NoError(t, p.Deregister(ctx, n.ID))

	// Node should no longer appear in the running list.
	running, err := p.List(ctx, "test-consul-dereg-svc", cluster.NodeStatusRunning)
	require.NoError(t, err)
	assert.Empty(t, running)
}

// TestClusterConsul_Watch fires the callback when a node is registered.
func TestClusterConsul_Watch(t *testing.T) {
	if os.Getenv("CORE_TEST_CONSUL") == "" {
		t.Skip("CORE_TEST_CONSUL not set")
	}
	p, err := cluster.NewConsulProvider(consulAddress())
	require.NoError(t, err)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fired := make(chan []*cluster.NodeInfo, 1)
	stopWatch, err := p.Watch(ctx, "test-consul-watch-svc", func(nodes []*cluster.NodeInfo) {
		select {
		case fired <- nodes:
		default:
		}
	})
	require.NoError(t, err)
	defer stopWatch()

	n := &cluster.NodeInfo{
		ID:           "test-consul-watch-node",
		ServiceName:  "test-consul-watch-svc",
		DataCenterID: 0,
		MachineID:    4,
		Address:      "127.0.0.1",
		Port:         9130,
		Weight:       1,
	}
	require.NoError(t, p.Register(ctx, n))
	defer p.Deregister(context.Background(), n.ID)

	select {
	case nodes := <-fired:
		assert.NotEmpty(t, nodes)
	case <-ctx.Done():
		t.Fatal("Watch callback not fired within timeout")
	}
}
