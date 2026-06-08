package cluster_test

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestNode returns a minimal NodeInfo for lifecycle tests.
func newTestNode(id, svc string, dc, machine int64) *cluster.NodeInfo {
	return &cluster.NodeInfo{
		ID:           id,
		ServiceName:  svc,
		DataCenterID: dc,
		MachineID:    machine,
		Address:      "127.0.0.1",
		Port:         9000 + int(machine),
		Weight:       1,
	}
}

// ============================================================
// Register / List / Deregister
// ============================================================

func TestLocalProvider_RegisterAndList(t *testing.T) {
	p := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	p.Start()
	defer p.Close()

	ctx := context.Background()
	node := newTestNode("svc-0-1", "orders", 0, 1)
	require.NoError(t, p.Register(ctx, node))

	nodes, err := p.List(ctx, "orders", cluster.NodeStatusRunning)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "svc-0-1", nodes[0].ID)
	assert.Equal(t, cluster.NodeStatusRunning, nodes[0].Status)
}

func TestLocalProvider_Deregister_MarksOffline(t *testing.T) {
	p := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	p.Start()
	defer p.Close()

	ctx := context.Background()
	node := newTestNode("svc-0-2", "orders", 0, 2)
	require.NoError(t, p.Register(ctx, node))
	require.NoError(t, p.Deregister(ctx, "svc-0-2"))

	nodes, err := p.List(ctx, "orders", cluster.NodeStatusOffline)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, cluster.NodeStatusOffline, nodes[0].Status)
}

// ============================================================
// Heartbeat keeps node alive
// ============================================================

func TestLocalProvider_Heartbeat_ResetsToRunning(t *testing.T) {
	// Use very short timeouts so fault detection fires quickly.
	heartbeatTimeout := 80 * time.Millisecond
	suspectTimeout := 80 * time.Millisecond
	p := cluster.NewLocalProvider(heartbeatTimeout, suspectTimeout, 30*time.Second)
	p.Start()
	defer p.Close()

	ctx := context.Background()
	node := newTestNode("svc-0-3", "payments", 0, 3)
	require.NoError(t, p.Register(ctx, node))

	// Send a heartbeat before the timeout fires; node should stay Running.
	time.Sleep(40 * time.Millisecond)
	require.NoError(t, p.Heartbeat(ctx, "svc-0-3"))
	time.Sleep(40 * time.Millisecond)
	require.NoError(t, p.Heartbeat(ctx, "svc-0-3"))

	nodes, err := p.List(ctx, "payments", cluster.NodeStatusRunning)
	require.NoError(t, err)
	assert.Len(t, nodes, 1, "node should remain Running after regular heartbeats")
}

// ============================================================
// Fault detection: running → suspect → offline
// ============================================================

func TestLocalProvider_FaultDetection_RunningToOffline(t *testing.T) {
	heartbeatTimeout := 60 * time.Millisecond
	suspectTimeout := 60 * time.Millisecond
	p := cluster.NewLocalProvider(heartbeatTimeout, suspectTimeout, 30*time.Second)
	p.Start()
	defer p.Close()

	ctx := context.Background()
	node := newTestNode("svc-0-4", "inventory", 0, 4)
	require.NoError(t, p.Register(ctx, node))

	// Wait long enough for fault detection to advance the node to offline
	// (heartbeatTimeout + suspectTimeout + 2 ticks of the background goroutine).
	deadline := time.Now().Add(500 * time.Millisecond)
	var offlineNodes []*cluster.NodeInfo
	for time.Now().Before(deadline) {
		nodes, err := p.List(ctx, "inventory", cluster.NodeStatusOffline)
		require.NoError(t, err)
		if len(nodes) > 0 {
			offlineNodes = nodes
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.Len(t, offlineNodes, 1, "node should become offline after missing heartbeats")
	assert.Equal(t, cluster.NodeStatusOffline, offlineNodes[0].Status)
}

// ============================================================
// MachineID isolation across service names
// ============================================================

func TestLocalProvider_MachineIDIsolation_DifferentServicesCanShareSlot(t *testing.T) {
	p := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	p.Start()
	defer p.Close()

	ctx := context.Background()
	// Two services can occupy DataCenterID=0, MachineID=1 simultaneously.
	fundNode := newTestNode("funds-0-1", "funds", 0, 1)
	orderNode := newTestNode("orders-0-1", "orders", 0, 1)

	require.NoError(t, p.Register(ctx, fundNode), "funds service should register at DC=0 MID=1")
	require.NoError(t, p.Register(ctx, orderNode), "orders service should register at same slot")

	allNodes, err := p.List(ctx, "", cluster.NodeStatusRunning)
	require.NoError(t, err)
	assert.Len(t, allNodes, 2)
}

func TestLocalProvider_MachineIDIsolation_SameServiceConflicts(t *testing.T) {
	p := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	p.Start()
	defer p.Close()

	ctx := context.Background()
	first := newTestNode("funds-0-1", "funds", 0, 1)
	second := newTestNode("funds-0-1b", "funds", 0, 1) // same service, same slot

	require.NoError(t, p.Register(ctx, first))
	err := p.Register(ctx, second)
	require.ErrorIs(t, err, cluster.ErrSlotConflict,
		"same service cannot occupy the same DataCenterID+MachineID slot")
}

// ============================================================
// MachineID cooldown: offline slot not immediately reusable
// ============================================================

func TestLocalProvider_Cooldown_PreventsEarlySlotReuse(t *testing.T) {
	cooldown := 200 * time.Millisecond
	p := cluster.NewLocalProvider(time.Second, time.Second, cooldown)
	p.Start()
	defer p.Close()

	ctx := context.Background()
	node := newTestNode("funds-0-5", "funds", 0, 5)
	require.NoError(t, p.Register(ctx, node))
	require.NoError(t, p.Deregister(ctx, "funds-0-5"))

	// Immediately try to reuse the slot — should fail during cooldown.
	reuse := newTestNode("funds-0-5b", "funds", 0, 5)
	err := p.Register(ctx, reuse)
	require.ErrorIs(t, err, cluster.ErrSlotConflict, "slot should be in cooldown immediately after offline")

	// After cooldown, slot should be reusable.
	time.Sleep(cooldown + 20*time.Millisecond)
	require.NoError(t, p.Register(ctx, reuse), "slot should be reusable after cooldown expires")
}

// ============================================================
// AllocateMachineID
// ============================================================

func TestLocalProvider_AllocateMachineID_FindsLowestFree(t *testing.T) {
	p := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	p.Start()
	defer p.Close()

	ctx := context.Background()
	// Occupy slots 0 and 1.
	require.NoError(t, p.Register(ctx, newTestNode("svc-0-0", "catalog", 0, 0)))
	require.NoError(t, p.Register(ctx, newTestNode("svc-0-1", "catalog", 0, 1)))

	id := p.AllocateMachineID("catalog", 0)
	assert.Equal(t, int64(2), id, "next free slot should be 2")
}

func TestLocalProvider_AllocateMachineID_ReturnsMinusOneWhenFull(t *testing.T) {
	p := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	p.Start()
	defer p.Close()

	ctx := context.Background()
	// Fill slots 0 and 1; cap at maxMachineID=1.
	require.NoError(t, p.Register(ctx, newTestNode("svc-0-0", "full-svc", 0, 0)))
	require.NoError(t, p.Register(ctx, newTestNode("svc-0-1", "full-svc", 0, 1)))

	id := p.AllocateMachineID("full-svc", 0, 1 /* maxMachineID */)
	assert.Equal(t, int64(-1), id, "should return -1 when all slots are occupied")
}

// ============================================================
// Watch callbacks
// ============================================================

func TestLocalProvider_Watch_FiresOnRegister(t *testing.T) {
	p := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	p.Start()
	defer p.Close()

	ctx := context.Background()
	changes := make(chan []*cluster.NodeInfo, 5)

	cancel, err := p.Watch(ctx, "watched-svc", func(nodes []*cluster.NodeInfo) {
		changes <- nodes
	})
	require.NoError(t, err)
	defer cancel()

	require.NoError(t, p.Register(ctx, newTestNode("w-0-1", "watched-svc", 0, 1)))

	select {
	case nodes := <-changes:
		assert.Len(t, nodes, 1)
	case <-time.After(time.Second):
		t.Fatal("Watch callback was not fired after Register")
	}
}

func TestLocalProvider_Watch_CancelStopsCallbacks(t *testing.T) {
	p := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	p.Start()
	defer p.Close()

	ctx := context.Background()
	changes := make(chan struct{}, 5)

	cancel, err := p.Watch(ctx, "cancel-svc", func(_ []*cluster.NodeInfo) {
		changes <- struct{}{}
	})
	require.NoError(t, err)

	// Drain initial change.
	require.NoError(t, p.Register(ctx, newTestNode("c-0-1", "cancel-svc", 0, 1)))
	<-changes

	cancel() // unwatch

	require.NoError(t, p.Register(ctx, newTestNode("c-0-2", "cancel-svc", 0, 2)))
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, len(changes), "no callbacks should fire after cancel")
}
