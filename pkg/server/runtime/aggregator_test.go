package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/runtime"
	"github.com/stretchr/testify/require"
)

type fakeCluster struct {
	nodes map[string][]*cluster.NodeInfo
}

func (f fakeCluster) List(_ context.Context, serviceName string, _ ...cluster.NodeStatus) ([]*cluster.NodeInfo, error) {
	return f.nodes[serviceName], nil
}

func (f fakeCluster) ListServices(context.Context) ([]string, error) {
	out := make([]string, 0, len(f.nodes))
	for k := range f.nodes {
		out = append(out, k)
	}
	return out, nil
}

type fakeProm struct {
	vectors map[string]float64
	err     error
}

func (f fakeProm) Query(_ context.Context, query string, _ time.Time) (runtime.Vector, error) {
	if f.err != nil {
		return nil, f.err
	}
	if v, ok := f.vectors[query]; ok {
		return runtime.Vector{{Value: v, Timestamp: time.Now()}}, nil
	}
	// 默认返回空向量
	return runtime.Vector{}, nil
}

func TestAggregatorTopologyMergesClusterAndMetrics(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"shop-user": {
			{ServiceName: "shop-user", Status: cluster.NodeStatusRunning, ID: "u1"},
		},
		"shop-order": {
			{ServiceName: "shop-order", Status: cluster.NodeStatusRunning, ID: "a"},
			{ServiceName: "shop-order", Status: cluster.NodeStatusRunning, ID: "b"},
		},
	}}
	rateQ, err := runtime.ServiceRequestRateQuery("shop-order", "15s")
	require.NoError(t, err)
	fp := fakeProm{vectors: map[string]float64{rateQ: 20}}
	agg := runtime.NewAggregator(fc, fp, runtime.Config{Mode: "prometheus"})
	resp, err := agg.Topology(context.Background(), "15s")
	require.NoError(t, err)
	require.Len(t, resp.Services, 2)
	var order *runtime.ServiceNode
	for i := range resp.Services {
		if resp.Services[i].Service == "shop-order" {
			order = &resp.Services[i]
		}
	}
	require.NotNil(t, order)
	require.Equal(t, 2, order.RunningInstances)
	require.NotNil(t, order.RequestRate.Value)
	require.Equal(t, 20.0, *order.RequestRate.Value)
	require.Equal(t, runtime.StateOK, order.State)
}

func TestAggregatorModeOffReturnsNotCollectedMetrics(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"shop-user": {{ServiceName: "shop-user", Status: cluster.NodeStatusRunning, ID: "u1"}},
	}}
	agg := runtime.NewAggregator(fc, nil, runtime.Config{Mode: "off"})
	resp, err := agg.Topology(context.Background(), "15s")
	require.NoError(t, err)
	require.Equal(t, runtime.StateNotCollected, resp.Services[0].RequestRate.State)
	require.Nil(t, resp.Services[0].RequestRate.Value)
}

func TestAggregatorPrometheusDownKeepsTopology(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"shop-user": {{ServiceName: "shop-user", Status: cluster.NodeStatusRunning, ID: "u1"}},
	}}
	fp := fakeProm{err: runtime.ErrPrometheusUnavailable}
	agg := runtime.NewAggregator(fc, fp, runtime.Config{Mode: "prometheus"})
	resp, err := agg.Topology(context.Background(), "15s")
	require.NoError(t, err)
	require.NotEmpty(t, resp.Services)
	require.Nil(t, resp.Services[0].RequestRate.Value)
	require.Equal(t, runtime.StateUnavailable, resp.Services[0].RequestRate.State)
}

func TestAggregatorKnownService(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"shop-order": {{ServiceName: "shop-order", Status: cluster.NodeStatusRunning}},
	}}
	agg := runtime.NewAggregator(fc, nil, runtime.Config{Mode: "off"})
	require.True(t, agg.KnownService(context.Background(), "shop-order"))
	require.False(t, agg.KnownService(context.Background(), "nope"))
}

func TestAggregatorBuildsSyncEdgesFromLabeledVector(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"shop-user":  {{ServiceName: "shop-user", Status: cluster.NodeStatusRunning}},
		"shop-order": {{ServiceName: "shop-order", Status: cluster.NodeStatusRunning}},
	}}
	edgeQ, err := runtime.ServiceCallEdgeRateQuery("15s")
	require.NoError(t, err)
	fp := labeledProm{samples: map[string]runtime.Vector{
		edgeQ: {
			{
				Value: 8,
				Metric: map[string]string{
					"source_service": "shop-user",
					"target_service": "shop-order",
					"protocol":       "grpc",
					"result_class":   "success",
				},
			},
			{
				Value: 2,
				Metric: map[string]string{
					"source_service": "shop-user",
					"target_service": "shop-order",
					"protocol":       "grpc",
					"result_class":   "server_error",
				},
			},
		},
	}}
	agg := runtime.NewAggregator(fc, fp, runtime.Config{Mode: "prometheus"})
	resp, err := agg.Topology(context.Background(), "15s")
	require.NoError(t, err)
	require.NotEmpty(t, resp.Edges)
	var sync *runtime.ServiceEdge
	for i := range resp.Edges {
		if resp.Edges[i].Kind == "sync" && resp.Edges[i].Source == "shop-user" && resp.Edges[i].Target == "shop-order" {
			sync = &resp.Edges[i]
			break
		}
	}
	require.NotNil(t, sync)
	require.NotNil(t, sync.RequestRate.Value)
	require.Equal(t, 10.0, *sync.RequestRate.Value)
	require.NotNil(t, sync.ErrorRate.Value)
	require.InDelta(t, 0.2, *sync.ErrorRate.Value, 0.001)
}

type labeledProm struct {
	samples map[string]runtime.Vector
	err     error
}

func (f labeledProm) Query(_ context.Context, query string, _ time.Time) (runtime.Vector, error) {
	if f.err != nil {
		return nil, f.err
	}
	if v, ok := f.samples[query]; ok {
		return v, nil
	}
	return runtime.Vector{}, nil
}
