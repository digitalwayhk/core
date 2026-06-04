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

// TestClusterLocal_DualNodeAutoMachineID simulates two replicas of the same
// service starting with the same config (same DataCenterID+MachineID).
// The second replica must automatically receive a different MachineID.
func TestClusterLocal_DualNodeAutoMachineID(t *testing.T) {
	if os.Getenv("CORE_TEST_CLUSTER_LOCAL") == "" {
		t.Skip("CORE_TEST_CLUSTER_LOCAL not set")
	}
	p := cluster.NewLocalProvider(2*time.Second, 2*time.Second, 500*time.Millisecond)
	p.Start()
	defer p.Close()
	ctx := context.Background()

	n1 := &cluster.NodeInfo{
		ID:           "svc-0-1-replica1",
		ServiceName:  "svc",
		DataCenterID: 0,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         8081,
		Weight:       1,
	}
	require.NoError(t, p.Register(ctx, n1))

	// Second replica tries same DataCenterID+MachineID → should conflict.
	n2 := &cluster.NodeInfo{
		ID:           "svc-0-1-replica2",
		ServiceName:  "svc",
		DataCenterID: 0,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         8082,
		Weight:       1,
	}
	err := p.Register(ctx, n2)
	assert.ErrorIs(t, err, cluster.ErrSlotConflict)

	// Auto-allocate next free slot (lowest ID not in use; 0 is free since n1 holds 1).
	newID := p.AllocateMachineID("svc", 0)
	assert.NotEqual(t, int64(1), newID, "auto-allocated ID must differ from the conflicting one")
	assert.GreaterOrEqual(t, newID, int64(0))

	n2.MachineID = newID
	n2.ID = fmt.Sprintf("svc-0-%d-replica2", newID)
	require.NoError(t, p.Register(ctx, n2))

	// Both nodes should be visible as running.
	running, err := p.List(ctx, "svc", cluster.NodeStatusRunning)
	require.NoError(t, err)
	assert.Len(t, running, 2)
	machineIDs := map[int64]bool{}
	for _, n := range running {
		machineIDs[n.MachineID] = true
	}
	assert.True(t, machineIDs[1], "machine 1 should be registered")
	assert.True(t, machineIDs[newID], "auto-allocated machine should be registered")
}

// TestClusterLocal_NodeOfflineAfterDeregister verifies that deregistering a node
// makes it appear offline in the registry.
func TestClusterLocal_NodeOfflineAfterDeregister(t *testing.T) {
	if os.Getenv("CORE_TEST_CLUSTER_LOCAL") == "" {
		t.Skip("CORE_TEST_CLUSTER_LOCAL not set")
	}
	p := cluster.NewLocalProvider(2*time.Second, 2*time.Second, 500*time.Millisecond)
	p.Start()
	defer p.Close()
	ctx := context.Background()

	n := &cluster.NodeInfo{
		ID:          "svc-0-1-node",
		ServiceName: "svc",
		MachineID:   1,
		Address:     "127.0.0.1",
		Port:        8080,
		Weight:      1,
	}
	require.NoError(t, p.Register(ctx, n))

	require.NoError(t, p.Deregister(ctx, n.ID))

	offline, err := p.List(ctx, "svc", cluster.NodeStatusOffline)
	require.NoError(t, err)
	assert.Len(t, offline, 1)
	assert.Equal(t, cluster.NodeStatusOffline, offline[0].Status)
}
