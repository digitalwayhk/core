package cluster_test

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterSwitcher_CompletePromotesPendingProvider(t *testing.T) {
	ctx := context.Background()
	provA := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provB := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provA.Start()
	provB.Start()
	defer provA.Close()
	defer provB.Close()

	switcher := cluster.NewClusterSwitcher(provA, "svc")
	require.NoError(t, switcher.Begin(ctx, provB))
	require.NoError(t, switcher.Complete(ctx))

	assert.Same(t, provB, switcher.Current())
	assert.Equal(t, provB.Name(), switcher.Current().Name())
}

func TestClusterSwitcher_BeginMigratesOnlyScopedServiceNodes(t *testing.T) {
	ctx := context.Background()
	provA := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provB := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provA.Start()
	provB.Start()
	defer provA.Close()
	defer provB.Close()

	require.NoError(t, provA.Register(ctx, &cluster.NodeInfo{
		ID:           "mysvc-1",
		ServiceName:  "mysvc",
		DataCenterID: 1,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         8080,
		Weight:       1,
	}))
	require.NoError(t, provA.Register(ctx, &cluster.NodeInfo{
		ID:           "othersvc-1",
		ServiceName:  "othersvc",
		DataCenterID: 1,
		MachineID:    2,
		Address:      "127.0.0.1",
		Port:         8081,
		Weight:       1,
	}))

	switcher := cluster.NewClusterSwitcher(provA, "mysvc")
	require.NoError(t, switcher.Begin(ctx, provB))

	nodes, err := provB.List(ctx, "mysvc")
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "mysvc-1", nodes[0].ID)

	otherNodes, err := provB.List(ctx, "othersvc")
	require.NoError(t, err)
	assert.Empty(t, otherNodes)
}
