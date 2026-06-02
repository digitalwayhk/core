package cluster_test

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeNodes(ids ...string) []*cluster.NodeInfo {
	nodes := make([]*cluster.NodeInfo, len(ids))
	for i, id := range ids {
		nodes[i] = &cluster.NodeInfo{ID: id, ServiceName: "svc", Weight: 1}
	}
	return nodes
}

// ---- RoundRobin ----

func TestRoundRobin_DistributesEvenly(t *testing.T) {
	b := cluster.NewRoundRobinBalancer()
	nodes := makeNodes("a", "b", "c")
	counts := map[string]int{}
	ctx := testCtx()
	for i := 0; i < 30; i++ {
		n, err := b.Pick(ctx, nodes, cluster.BalanceHint{})
		require.NoError(t, err)
		counts[n.ID]++
	}
	for _, c := range counts {
		assert.Equal(t, 10, c)
	}
}

func TestRoundRobin_EmptyReturnsError(t *testing.T) {
	b := cluster.NewRoundRobinBalancer()
	_, err := b.Pick(testCtx(), nil, cluster.BalanceHint{})
	assert.ErrorIs(t, err, cluster.ErrEmptyCandidates)
}

// ---- LocalFirst ----

func TestLocalFirst_PrefersLocalNode(t *testing.T) {
	b := cluster.NewLocalFirstBalancer()
	nodes := makeNodes("a", "b", "local-node")
	n, err := b.Pick(testCtx(), nodes, cluster.BalanceHint{LocalNodeID: "local-node"})
	require.NoError(t, err)
	assert.Equal(t, "local-node", n.ID)
}

func TestLocalFirst_FallsBackToRoundRobin(t *testing.T) {
	b := cluster.NewLocalFirstBalancer()
	nodes := makeNodes("a", "b")
	// local-node not in list
	n, err := b.Pick(testCtx(), nodes, cluster.BalanceHint{LocalNodeID: "not-in-list"})
	require.NoError(t, err)
	assert.NotEmpty(t, n.ID)
}

// ---- ConsistentHash ----

func TestConsistentHash_SameKeyReturnsSameNode(t *testing.T) {
	b := cluster.NewConsistentHashBalancer()
	nodes := makeNodes("a", "b", "c", "d")
	hint := cluster.BalanceHint{HashKey: "user-42"}
	n1, err := b.Pick(testCtx(), nodes, hint)
	require.NoError(t, err)
	n2, err := b.Pick(testCtx(), nodes, hint)
	require.NoError(t, err)
	assert.Equal(t, n1.ID, n2.ID)
}

func TestConsistentHash_DifferentKeysMayReturnDifferentNodes(t *testing.T) {
	b := cluster.NewConsistentHashBalancer()
	nodes := makeNodes("a", "b", "c", "d")
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		n, err := b.Pick(testCtx(), nodes, cluster.BalanceHint{HashKey: string(rune('0' + i))})
		require.NoError(t, err)
		seen[n.ID] = true
	}
	// With 20 different keys across 4 nodes we expect multiple nodes to be chosen.
	assert.Greater(t, len(seen), 1)
}

// ---- Weighted ----

func TestWeighted_HigherWeightPickedMore(t *testing.T) {
	b := cluster.NewWeightedBalancer()
	nodes := []*cluster.NodeInfo{
		{ID: "heavy", ServiceName: "svc", Weight: 9},
		{ID: "light", ServiceName: "svc", Weight: 1},
	}
	counts := map[string]int{}
	for i := 0; i < 100; i++ {
		n, err := b.Pick(testCtx(), nodes, cluster.BalanceHint{})
		require.NoError(t, err)
		counts[n.ID]++
	}
	assert.Greater(t, counts["heavy"], counts["light"])
}
