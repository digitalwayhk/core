package cluster_test

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildProvider_Off(t *testing.T) {
	sharedLocal := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provider, err := cluster.BuildProvider(&config.ClusterConfig{Mode: "off"}, sharedLocal)
	require.NoError(t, err)
	assert.Nil(t, provider)
}

func TestBuildProvider_Local(t *testing.T) {
	sharedLocal := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provider, err := cluster.BuildProvider(&config.ClusterConfig{Mode: "auto", Provider: "local"}, sharedLocal)
	require.NoError(t, err)
	assert.Same(t, sharedLocal, provider)
}

func TestBuildProvider_AutoFallback(t *testing.T) {
	sharedLocal := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provider, err := cluster.BuildProvider(&config.ClusterConfig{Mode: "auto", Provider: "etcd"}, sharedLocal)
	require.NoError(t, err)
	assert.Same(t, sharedLocal, provider)
}

func TestBuildProvider_ModeOnReturnsError(t *testing.T) {
	sharedLocal := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provider, err := cluster.BuildProvider(&config.ClusterConfig{
		Mode:     "on",
		Provider: "etcd",
		Providers: config.ClusterProviderConfig{
			Etcd: config.EtcdProviderConfig{Endpoints: []string{}},
		},
	}, sharedLocal)
	require.Error(t, err)
	assert.Nil(t, provider)
}

// TestBuildProvider_ModeOnEtcdEmptyEndpoints verifies that Mode=on with an
// external provider that has no endpoints returns an error (not a silent fallback).
func TestBuildProvider_ModeOnEtcdEmptyEndpoints(t *testing.T) {
	sharedLocal := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	_, err := cluster.BuildProvider(&config.ClusterConfig{
		Mode:     "on",
		Provider: "etcd",
		// Endpoints intentionally empty to trigger validation.
	}, sharedLocal)
	assert.Error(t, err, "mode=on with empty etcd endpoints must return an error")
}

// TestBuildProvider_ModeAutoEtcdEmptyEndpoints verifies that Mode=auto with
// empty etcd endpoints gracefully falls back to the local provider.
func TestBuildProvider_ModeAutoEtcdEmptyEndpoints(t *testing.T) {
	sharedLocal := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provider, err := cluster.BuildProvider(&config.ClusterConfig{
		Mode:     "auto",
		Provider: "etcd",
	}, sharedLocal)
	require.NoError(t, err)
	assert.Same(t, sharedLocal, provider, "mode=auto should fall back to local when etcd is unavailable")
}

// TestNewClusterSwitcher_ScopedToServiceName verifies that Begin() only copies
// nodes belonging to the given serviceName, not all nodes.
func TestNewClusterSwitcher_ScopedToServiceName(t *testing.T) {
	local := cluster.NewLocalProvider(10*time.Second, 10*time.Second, 30*time.Second)
	local.Start()
	defer local.Close()

	ctx := context.Background()

	// Register two services.
	nodeA := &cluster.NodeInfo{
		ID: "svc-a-0-1", ServiceName: "svc-a",
		DataCenterID: 0, MachineID: 1,
		Address: "127.0.0.1", Port: 9001, Weight: 1,
	}
	nodeB := &cluster.NodeInfo{
		ID: "svc-b-0-2", ServiceName: "svc-b",
		DataCenterID: 0, MachineID: 2,
		Address: "127.0.0.1", Port: 9002, Weight: 1,
	}
	require.NoError(t, local.Register(ctx, nodeA))
	require.NoError(t, local.Register(ctx, nodeB))

	// Build a switcher scoped to svc-a only.
	dst := cluster.NewLocalProvider(10*time.Second, 10*time.Second, 30*time.Second)
	dst.Start()
	defer dst.Close()

	switcher := cluster.NewClusterSwitcher(local, "svc-a")
	require.NoError(t, switcher.Begin(ctx, dst))

	// dst should have svc-a nodes but NOT svc-b nodes.
	nodesA, err := dst.List(ctx, "svc-a")
	require.NoError(t, err)
	assert.Len(t, nodesA, 1)
	assert.Equal(t, "svc-a", nodesA[0].ServiceName)

	nodesB, err := dst.List(ctx, "svc-b")
	require.NoError(t, err)
	assert.Len(t, nodesB, 0, "switcher scoped to svc-a should not copy svc-b nodes")
}
