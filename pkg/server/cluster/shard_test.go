package cluster_test

import (
	"context"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCtx() context.Context { return context.Background() }

func makeMetaNodes(specs []struct{ id, val string }) []*cluster.NodeInfo {
	nodes := make([]*cluster.NodeInfo, len(specs))
	for i, s := range specs {
		nodes[i] = &cluster.NodeInfo{
			ID:          s.id,
			ServiceName: "svc",
			Metadata:    map[string]string{"accountTypeid": s.val},
			Weight:      1,
		}
	}
	return nodes
}

func TestShardRouter_NoRule_ReturnsAll(t *testing.T) {
	r := cluster.NewServiceShardRouter()
	nodes := makeNodes("a", "b", "c")
	result, err := r.Filter("unknown-svc", nodes, nil)
	require.NoError(t, err)
	assert.Equal(t, nodes, result)
}

func TestShardRouter_ExactMatch(t *testing.T) {
	r := cluster.NewServiceShardRouter()
	r.AddRule("svc", cluster.ServiceShardRule{
		MetadataKey: "accountTypeid",
		PayloadKey:  "accountTypeid",
		Mode:        cluster.ShardModeExact,
		MissingKey:  cluster.MissingKeyPolicyFallback,
		EmptyCandidate: cluster.EmptyCandidatePolicyFallback,
	})
	nodes := makeMetaNodes([]struct{ id, val string }{
		{"spot-1", "spot"},
		{"spot-2", "spot"},
		{"contract-1", "contract"},
	})

	result, err := r.Filter("svc", nodes, map[string]string{"accountTypeid": "spot"})
	require.NoError(t, err)
	assert.Len(t, result, 2)
	for _, n := range result {
		assert.Equal(t, "spot", n.Metadata["accountTypeid"])
	}
}

func TestShardRouter_ExactMatch_Contract(t *testing.T) {
	r := cluster.NewServiceShardRouter()
	r.AddRule("svc", cluster.ServiceShardRule{
		MetadataKey:    "accountTypeid",
		PayloadKey:     "accountTypeid",
		Mode:           cluster.ShardModeExact,
		MissingKey:     cluster.MissingKeyPolicyFallback,
		EmptyCandidate: cluster.EmptyCandidatePolicyFallback,
	})
	nodes := makeMetaNodes([]struct{ id, val string }{
		{"spot-1", "spot"},
		{"contract-1", "contract"},
		{"contract-2", "contract"},
	})

	result, err := r.Filter("svc", nodes, map[string]string{"accountTypeid": "contract"})
	require.NoError(t, err)
	assert.Len(t, result, 2)
	for _, n := range result {
		assert.Equal(t, "contract", n.Metadata["accountTypeid"])
	}
}

func TestShardRouter_MissingKeyError(t *testing.T) {
	r := cluster.NewServiceShardRouter()
	r.AddRule("svc", cluster.ServiceShardRule{
		MetadataKey: "accountTypeid",
		PayloadKey:  "accountTypeid",
		Mode:        cluster.ShardModeExact,
		MissingKey:  cluster.MissingKeyPolicyError,
		EmptyCandidate: cluster.EmptyCandidatePolicyFallback,
	})
	nodes := makeMetaNodes([]struct{ id, val string }{{"n1", "spot"}})

	_, err := r.Filter("svc", nodes, map[string]string{}) // key absent
	assert.Error(t, err)
}

func TestShardRouter_EmptyCandidateError(t *testing.T) {
	r := cluster.NewServiceShardRouter()
	r.AddRule("svc", cluster.ServiceShardRule{
		MetadataKey:    "accountTypeid",
		PayloadKey:     "accountTypeid",
		Mode:           cluster.ShardModeExact,
		MissingKey:     cluster.MissingKeyPolicyFallback,
		EmptyCandidate: cluster.EmptyCandidatePolicyError,
	})
	nodes := makeMetaNodes([]struct{ id, val string }{{"n1", "spot"}})

	_, err := r.Filter("svc", nodes, map[string]string{"accountTypeid": "contract"})
	assert.ErrorIs(t, err, cluster.ErrEmptyCandidates)
}

func TestShardRouter_MissingKeyFallback_ReturnsAll(t *testing.T) {
	r := cluster.NewServiceShardRouter()
	r.AddRule("svc", cluster.ServiceShardRule{
		MetadataKey:    "accountTypeid",
		PayloadKey:     "accountTypeid",
		Mode:           cluster.ShardModeExact,
		MissingKey:     cluster.MissingKeyPolicyFallback,
		EmptyCandidate: cluster.EmptyCandidatePolicyFallback,
	})
	nodes := makeMetaNodes([]struct{ id, val string }{{"n1", "spot"}, {"n2", "contract"}})

	result, err := r.Filter("svc", nodes, map[string]string{})
	require.NoError(t, err)
	assert.Len(t, result, 2)
}
