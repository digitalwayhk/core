package cluster_test

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestProvider() *cluster.LocalProvider {
	return cluster.NewLocalProvider(200*time.Millisecond, 200*time.Millisecond, 100*time.Millisecond)
}

func newNode(id, service string, dc, machine int64) *cluster.NodeInfo {
	return &cluster.NodeInfo{
		ID:          id,
		ServiceName: service,
		DataCenterID: dc,
		MachineID:   machine,
		Address:     "127.0.0.1",
		Port:        8080,
		Weight:      1,
	}
}

// --- Registration ---

func TestLocalProvider_RegisterAndList(t *testing.T) {
	p := newTestProvider()
	defer p.Close()

	ctx := context.Background()
	node := newNode("svc-0-1-1234", "svc", 0, 1)
	require.NoError(t, p.Register(ctx, node))

	list, err := p.List(ctx, "svc")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "svc-0-1-1234", list[0].ID)
	assert.Equal(t, cluster.NodeStatusRunning, list[0].Status)
}

func TestLocalProvider_ListByStatus(t *testing.T) {
	p := newTestProvider()
	defer p.Close()
	ctx := context.Background()

	require.NoError(t, p.Register(ctx, newNode("n1", "svc", 0, 1)))
	require.NoError(t, p.Register(ctx, newNode("n2", "svc", 0, 2)))
	require.NoError(t, p.Deregister(ctx, "n2"))

	running, err := p.List(ctx, "svc", cluster.NodeStatusRunning)
	require.NoError(t, err)
	assert.Len(t, running, 1)
	assert.Equal(t, "n1", running[0].ID)

	offline, err := p.List(ctx, "svc", cluster.NodeStatusOffline)
	require.NoError(t, err)
	assert.Len(t, offline, 1)
	assert.Equal(t, "n2", offline[0].ID)
}

func TestLocalProvider_Register_SlotConflict(t *testing.T) {
	p := newTestProvider()
	defer p.Close()
	ctx := context.Background()

	require.NoError(t, p.Register(ctx, newNode("n1", "svc", 0, 5)))
	err := p.Register(ctx, newNode("n2", "svc", 0, 5)) // same dc+machine, different ID
	assert.ErrorIs(t, err, cluster.ErrSlotConflict)
}

func TestLocalProvider_Register_ReuseOfflineSlot(t *testing.T) {
	p := cluster.NewLocalProvider(50*time.Millisecond, 50*time.Millisecond, 1*time.Millisecond)
	defer p.Close()
	ctx := context.Background()

	require.NoError(t, p.Register(ctx, newNode("n1", "svc", 0, 5)))
	require.NoError(t, p.Deregister(ctx, "n1"))

	time.Sleep(10 * time.Millisecond) // past cooldown

	// A different node may now claim the slot.
	err := p.Register(ctx, newNode("n2", "svc", 0, 5))
	assert.NoError(t, err)
}

func TestLocalProvider_Heartbeat_Resets_Status(t *testing.T) {
	p := cluster.NewLocalProvider(50*time.Millisecond, 200*time.Millisecond, 10*time.Millisecond)
	p.Start()
	defer p.Close()
	ctx := context.Background()

	require.NoError(t, p.Register(ctx, newNode("n1", "svc", 0, 1)))

	// Wait long enough for the node to become suspect.
	time.Sleep(120 * time.Millisecond)

	n, err := p.Get(ctx, "n1")
	require.NoError(t, err)
	assert.Equal(t, cluster.NodeStatusSuspect, n.Status)

	// Heartbeat should restore running status.
	require.NoError(t, p.Heartbeat(ctx, "n1"))
	n, err = p.Get(ctx, "n1")
	require.NoError(t, err)
	assert.Equal(t, cluster.NodeStatusRunning, n.Status)
}

func TestLocalProvider_FaultDetection_OfflineAfterSuspect(t *testing.T) {
	p := cluster.NewLocalProvider(50*time.Millisecond, 50*time.Millisecond, 10*time.Millisecond)
	p.Start()
	defer p.Close()
	ctx := context.Background()

	require.NoError(t, p.Register(ctx, newNode("n1", "svc", 0, 1)))

	// Wait long enough to pass heartbeat + suspect timeout.
	time.Sleep(300 * time.Millisecond)

	n, err := p.Get(ctx, "n1")
	require.NoError(t, err)
	assert.Equal(t, cluster.NodeStatusOffline, n.Status)
}

func TestLocalProvider_Watch(t *testing.T) {
	p := newTestProvider()
	defer p.Close()
	ctx := context.Background()

	events := make(chan []*cluster.NodeInfo, 5)
	cancel, err := p.Watch(ctx, "svc", func(nodes []*cluster.NodeInfo) {
		events <- nodes
	})
	require.NoError(t, err)
	defer cancel()

	require.NoError(t, p.Register(ctx, newNode("n1", "svc", 0, 1)))

	select {
	case nodes := <-events:
		assert.Len(t, nodes, 1)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for watch event")
	}
}

func TestLocalProvider_WatchCancel(t *testing.T) {
	p := newTestProvider()
	defer p.Close()
	ctx := context.Background()

	events := make(chan struct{}, 5)
	cancel, _ := p.Watch(ctx, "svc", func(_ []*cluster.NodeInfo) {
		events <- struct{}{}
	})

	p.Register(ctx, newNode("n1", "svc", 0, 1))
	<-events // first event received

	cancel() // unsubscribe

	p.Register(ctx, newNode("n2", "svc", 0, 2))
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, len(events), "no event after cancel")
}

// --- AllocateMachineID ---

func TestLocalProvider_AllocateMachineID(t *testing.T) {
	p := newTestProvider()
	defer p.Close()
	ctx := context.Background()

	require.NoError(t, p.Register(ctx, newNode("n0", "svc", 0, 0)))
	require.NoError(t, p.Register(ctx, newNode("n1", "svc", 0, 1)))

	id := p.AllocateMachineID(0)
	assert.Equal(t, int64(2), id)
}
