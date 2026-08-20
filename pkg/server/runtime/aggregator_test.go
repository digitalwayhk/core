package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/runtime"
	"github.com/stretchr/testify/require"
)

type fakeCluster struct {
	nodes map[string][]*cluster.NodeInfo
}

type duplicateFailCluster struct{}

func (duplicateFailCluster) List(context.Context, string, ...cluster.NodeStatus) ([]*cluster.NodeInfo, error) {
	return nil, errors.New("cluster list failed")
}

func (duplicateFailCluster) ListServices(context.Context) ([]string, error) {
	return []string{"positions", "positions"}, nil
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
	rateQ, err := runtime.ServiceHTTPRateByCodeQuery("shop-order", "15s")
	require.NoError(t, err)
	coreQ, err := runtime.ServiceCoreRateByResultQuery("shop-order", "15s")
	require.NoError(t, err)
	fp := labeledProm{samples: map[string]runtime.Vector{
		rateQ: {{Value: 20, Metric: map[string]string{"code": "200"}}},
		coreQ: {},
	}}
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

func TestAggregatorEmptyVectorIsNotCollectedNotZero(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"shop-user": {{ServiceName: "shop-user", Status: cluster.NodeStatusRunning, ID: "u1"}},
	}}
	// prometheus mode but all queries return empty vectors
	agg := runtime.NewAggregator(fc, labeledProm{samples: map[string]runtime.Vector{}}, runtime.Config{Mode: "prometheus"})
	resp, err := agg.Topology(context.Background(), "15s")
	require.NoError(t, err)
	require.Nil(t, resp.Services[0].RequestRate.Value)
	require.Equal(t, runtime.StateNotCollected, resp.Services[0].RequestRate.State)
	require.NotEqual(t, runtime.StateOK, resp.Services[0].State)
}

func TestAggregatorAsyncEdgesJoinPublishAndSubscriptions(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"shop-order": {{ServiceName: "shop-order", Status: cluster.NodeStatusRunning}},
		"shop-user":  {{ServiceName: "shop-user", Status: cluster.NodeStatusRunning}},
	}}
	idx := runtime.NewMemorySubscriptionIndex()
	unregister := idx.Register("shop-user", "order.changed", "OrderCreated", true)
	t.Cleanup(unregister)
	pubQ, err := runtime.EventPublishRateQuery("15s")
	require.NoError(t, err)
	fp := labeledProm{samples: map[string]runtime.Vector{
		pubQ: {{
			Value: 5,
			Metric: map[string]string{
				"source_service": "shop-order",
				"subject_family": "order.changed",
				"event_type":     "ordercreated",
				"result_class":   "success",
			},
		}},
	}}
	agg := runtime.NewAggregator(fc, fp, runtime.Config{Mode: "prometheus"})
	agg.SetSubscriptions(idx)
	resp, err := agg.Topology(context.Background(), "15s")
	require.NoError(t, err)
	var async *runtime.ServiceEdge
	for i := range resp.Edges {
		if resp.Edges[i].Kind == "async" {
			async = &resp.Edges[i]
			break
		}
	}
	require.NotNil(t, async)
	require.Equal(t, "shop-order", async.Source)
	require.Equal(t, "shop-user", async.Target)
	require.NotNil(t, async.RequestRate.Value)
	require.Equal(t, 5.0, *async.RequestRate.Value)
}

func TestAggregatorAsyncEdgesKeepAllPublishers(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"shop-user": {{ServiceName: "shop-user", Status: cluster.NodeStatusRunning}},
	}}
	idx := runtime.NewMemorySubscriptionIndex()
	t.Cleanup(idx.Register("shop-user", "order.changed", "OrderCreated", true))
	pubQ, err := runtime.EventPublishRateQuery("15s")
	require.NoError(t, err)
	fp := labeledProm{samples: map[string]runtime.Vector{
		pubQ: {
			{Value: 5, Metric: map[string]string{"source_service": "shop-order", "subject_family": "order.changed", "event_type": "ordercreated", "result_class": "success"}},
			{Value: 3, Metric: map[string]string{"source_service": "shop-order-b", "subject_family": "order.changed", "event_type": "ordercreated", "result_class": "success"}},
		},
	}}
	agg := runtime.NewAggregator(fc, fp, runtime.Config{Mode: "prometheus"})
	agg.SetSubscriptions(idx)
	resp, err := agg.Topology(context.Background(), "15s")
	require.NoError(t, err)
	sources := map[string]bool{}
	for _, e := range resp.Edges {
		if e.Kind == "async" {
			sources[e.Source] = true
		}
	}
	require.True(t, sources["shop-order"])
	require.True(t, sources["shop-order-b"], "must keep all publish sources, not only max rate")
}

func TestAggregatorAsyncEdgesAggregateEventTypes(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"shop-user": {{ServiceName: "shop-user", Status: cluster.NodeStatusRunning}},
	}}
	idx := runtime.NewMemorySubscriptionIndex()
	t.Cleanup(idx.Register("shop-user", "order.changed", "OrderCreated", true))
	t.Cleanup(idx.Register("shop-user", "order.changed", "OrderStatusChanged", true))
	t.Cleanup(idx.Register("shop-user", "order.changed", "PaymentChanged", true))
	pubQ, err := runtime.EventPublishRateQuery("15s")
	require.NoError(t, err)
	fp := labeledProm{samples: map[string]runtime.Vector{
		pubQ: {
			{Value: 2, Metric: map[string]string{"source_service": "shop-order", "subject_family": "order.changed", "event_type": "ordercreated", "result_class": "success"}},
			{Value: 3, Metric: map[string]string{"source_service": "shop-order", "subject_family": "order.changed", "event_type": "orderstatuschanged", "result_class": "success"}},
			{Value: 1, Metric: map[string]string{"source_service": "shop-order", "subject_family": "order.changed", "event_type": "paymentchanged", "result_class": "success"}},
		},
	}}
	agg := runtime.NewAggregator(fc, fp, runtime.Config{Mode: "prometheus"})
	agg.SetSubscriptions(idx)
	resp, err := agg.Topology(context.Background(), "15s")
	require.NoError(t, err)
	var asyncCount int
	var rate float64
	for _, e := range resp.Edges {
		if e.Kind == "async" && e.Source == "shop-order" && e.Target == "shop-user" {
			asyncCount++
			if e.RequestRate.Value != nil {
				rate = *e.RequestRate.Value
			}
		}
	}
	require.Equal(t, 1, asyncCount, "must aggregate multiple event types into one edge")
	require.Equal(t, 6.0, rate)
}

// TestAggregatorAsyncIdleSubscriptionIsNotCollectedWithoutWarning 验证从未发布的空闲订阅只保留诚实状态，不产生故障告警。
func TestAggregatorAsyncIdleSubscriptionIsNotCollectedWithoutWarning(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"positions": {{ServiceName: "positions", Status: cluster.NodeStatusRunning}},
	}}
	idx := runtime.NewMemorySubscriptionIndex()
	t.Cleanup(idx.Register("positions", "funds.settlement.rejected", "SettlementRejected", true))

	agg := runtime.NewAggregator(fc, labeledProm{samples: map[string]runtime.Vector{}}, runtime.Config{Mode: "prometheus"})
	agg.SetSubscriptions(idx)
	resp, err := agg.Topology(context.Background(), "5m")

	require.NoError(t, err)
	require.Len(t, resp.Edges, 1)
	require.Equal(t, "async", resp.Edges[0].Kind)
	require.Empty(t, resp.Edges[0].Source)
	require.Equal(t, "positions", resp.Edges[0].Target)
	require.Equal(t, "funds.settlement.rejected", resp.Edges[0].SubjectFamily)
	require.Equal(t, runtime.StateNotCollected, resp.Edges[0].State)
	require.Nil(t, resp.Edges[0].RequestRate.Value)
	for _, warning := range resp.Warnings {
		require.NotEqual(t, "async_publish_missing", warning.Code)
	}
}

// TestAggregatorAsyncExistingSeriesWithZeroRateIsNoTraffic 验证发布序列存在但窗口速率为零时标记无流量，不误报发布缺失。
func TestAggregatorAsyncExistingSeriesWithZeroRateIsNoTraffic(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"funds":     {{ServiceName: "funds", Status: cluster.NodeStatusRunning}},
		"positions": {{ServiceName: "positions", Status: cluster.NodeStatusRunning}},
	}}
	idx := runtime.NewMemorySubscriptionIndex()
	t.Cleanup(idx.Register("positions", "positions.position.updated", "PositionUpdated", true))
	pubQ, err := runtime.EventPublishRateQuery("5m")
	require.NoError(t, err)
	fp := labeledProm{samples: map[string]runtime.Vector{
		pubQ: {{
			Value: 0,
			Metric: map[string]string{
				"source_service": "funds",
				"subject_family": "positions.position.updated",
				"event_type":     "positionupdated",
				"result_class":   "success",
			},
		}},
	}}

	agg := runtime.NewAggregator(fc, fp, runtime.Config{Mode: "prometheus"})
	agg.SetSubscriptions(idx)
	resp, err := agg.Topology(context.Background(), "5m")

	require.NoError(t, err)
	require.Len(t, resp.Edges, 1)
	require.Equal(t, "funds", resp.Edges[0].Source)
	require.Equal(t, runtime.StateNoTraffic, resp.Edges[0].State)
	require.NotNil(t, resp.Edges[0].RequestRate.Value)
	require.Zero(t, *resp.Edges[0].RequestRate.Value)
	for _, warning := range resp.Warnings {
		require.NotEqual(t, "async_publish_missing", warning.Code)
	}
}

// TestAggregatorAsyncPublishWithoutSubscriptionWarnsWithoutInventingEdge 验证真实发布缺少订阅时保留规格告警，但不猜测目标节点。
func TestAggregatorAsyncPublishWithoutSubscriptionWarnsWithoutInventingEdge(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"pricing": {{ServiceName: "pricing", Status: cluster.NodeStatusRunning}},
	}}
	pubQ, err := runtime.EventPublishRateQuery("5m")
	require.NoError(t, err)
	fp := labeledProm{samples: map[string]runtime.Vector{
		pubQ: {{
			Value: 2,
			Metric: map[string]string{
				"source_service": "pricing",
				"subject_family": "pricing.price.snapshot",
				"event_type":     "pricesnapshot",
				"result_class":   "success",
			},
		}},
	}}

	agg := runtime.NewAggregator(fc, fp, runtime.Config{Mode: "prometheus"})
	resp, err := agg.Topology(context.Background(), "5m")

	require.NoError(t, err)
	require.Empty(t, resp.Edges)
	require.Len(t, resp.Warnings, 1)
	require.Equal(t, "async_subscription_missing", resp.Warnings[0].Code)
	require.Contains(t, resp.Warnings[0].Message, "pricing.price.snapshot")
	require.Contains(t, resp.Warnings[0].Message, "pricesnapshot")
	require.Contains(t, resp.Warnings[0].Scope, "pricing.price.snapshot")
}

// TestAggregatorAsyncIdleFamiliesRemainDistinctWithoutWarningFlood 验证同一订阅方的多个空闲 family 各自成边且不重复刷缺发布告警。
func TestAggregatorAsyncIdleFamiliesRemainDistinctWithoutWarningFlood(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"positions": {{ServiceName: "positions", Status: cluster.NodeStatusRunning}},
	}}
	idx := runtime.NewMemorySubscriptionIndex()
	t.Cleanup(idx.Register("positions", "funds.settlement.rejected", "SettlementRejected", true))
	t.Cleanup(idx.Register("positions", "positions.liquidation.tick", "LiquidationTick", true))

	agg := runtime.NewAggregator(fc, labeledProm{samples: map[string]runtime.Vector{}}, runtime.Config{Mode: "prometheus"})
	agg.SetSubscriptions(idx)
	resp, err := agg.Topology(context.Background(), "5m")

	require.NoError(t, err)
	require.Len(t, resp.Edges, 2)
	families := map[string]bool{}
	for _, edge := range resp.Edges {
		families[edge.SubjectFamily] = true
		require.Equal(t, runtime.StateNotCollected, edge.State)
	}
	require.True(t, families["funds.settlement.rejected"])
	require.True(t, families["positions.liquidation.tick"])
	for _, warning := range resp.Warnings {
		require.NotEqual(t, "async_publish_missing", warning.Code)
	}
}

// TestAggregatorTopologyDeduplicatesWarnings 验证同一轮拓扑响应按 code、scope、message 去重告警。
func TestAggregatorTopologyDeduplicatesWarnings(t *testing.T) {
	agg := runtime.NewAggregator(duplicateFailCluster{}, nil, runtime.Config{Mode: "off"})
	resp, err := agg.Topology(context.Background(), "5m")

	require.NoError(t, err)
	require.Len(t, resp.Warnings, 1)
	require.Equal(t, "cluster_partial", resp.Warnings[0].Code)
	require.Equal(t, "positions", resp.Warnings[0].Scope)
}

func TestSubscriptionCancelIsIdempotent(t *testing.T) {
	idx := runtime.NewMemorySubscriptionIndex()
	cancel := idx.Register("shop-user", "order.changed", "OrderCreated", true)
	cancel()
	cancel() // must not panic or underflow
	list, err := idx.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestSubscriptionUnregisterRemovesEdge(t *testing.T) {
	idx := runtime.NewMemorySubscriptionIndex()
	cancel := idx.Register("shop-user", "order.changed", "OrderCreated", true)
	list, err := idx.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	cancel()
	list, err = idx.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, list)
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
	warningCodes := map[string]bool{}
	for _, warning := range resp.Warnings {
		warningCodes[warning.Code] = true
	}
	require.True(t, warningCodes["event_publish_query_partial"])
	require.False(t, warningCodes["async_publish_missing"])
}

func TestAggregatorKnownService(t *testing.T) {
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		"shop-order": {{ServiceName: "shop-order", Status: cluster.NodeStatusRunning}},
	}}
	agg := runtime.NewAggregator(fc, nil, runtime.Config{Mode: "off"})
	require.True(t, agg.KnownService(context.Background(), "shop-order"))
	require.False(t, agg.KnownService(context.Background(), "nope"))
}

func TestServiceDetailTreatsZeroRouteRateAsCollected(t *testing.T) {
	const (
		service = "shop-order"
		window  = "5m"
		route   = "/api/shop-order/getorders"
	)
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		service: {{ServiceName: service, Status: cluster.NodeStatusRunning}},
	}}
	coreRateQ, err := runtime.ServiceCoreRateByResultQuery(service, window)
	require.NoError(t, err)
	routeRateQ, err := runtime.ServiceRouteRateQuery(service, window)
	require.NoError(t, err)
	fp := labeledProm{samples: map[string]runtime.Vector{
		coreRateQ: {{
			Value:  0,
			Metric: map[string]string{"result_class": "success"},
		}},
		routeRateQ: {{
			Value:  0,
			Metric: map[string]string{"route": route, "result_class": "success"},
		}},
	}}

	agg := runtime.NewAggregator(fc, fp, runtime.Config{Mode: "prometheus"})
	detail, err := agg.ServiceDetail(context.Background(), window, service)

	require.NoError(t, err)
	require.Len(t, detail.Routes, 1)
	require.Equal(t, runtime.StateNoTraffic, detail.Routes[0].State)
	require.NotNil(t, detail.Routes[0].RequestRate.Value)
	require.Zero(t, *detail.Routes[0].RequestRate.Value)
	require.NotNil(t, detail.Routes[0].ErrorRate.Value)
	require.Zero(t, *detail.Routes[0].ErrorRate.Value)
	require.Equal(t, runtime.StateNoTraffic, detail.Routes[0].P50Ms.State)
	require.Equal(t, runtime.StateNoTraffic, detail.Routes[0].P95Ms.State)
	require.Equal(t, runtime.StateNoTraffic, detail.Routes[0].P99Ms.State)
}

func TestServiceDetailUsesCoreDurationQuantiles(t *testing.T) {
	const (
		service = "shop-order"
		window  = "5m"
		route   = "/api/shop-order/createorder"
	)
	fc := fakeCluster{nodes: map[string][]*cluster.NodeInfo{
		service: {{ServiceName: service, Status: cluster.NodeStatusRunning}},
	}}
	coreRateQ, err := runtime.ServiceCoreRateByResultQuery(service, window)
	require.NoError(t, err)
	routeRateQ, err := runtime.ServiceRouteRateQuery(service, window)
	require.NoError(t, err)
	samples := map[string]runtime.Vector{
		coreRateQ: {{
			Value:  2,
			Metric: map[string]string{"result_class": "success"},
		}},
		routeRateQ: {{
			Value:  2,
			Metric: map[string]string{"route": route, "result_class": "success"},
		}},
	}
	quantiles := []struct {
		q     float64
		value float64
	}{
		{q: 0.50, value: 4},
		{q: 0.95, value: 12},
		{q: 0.99, value: 20},
	}
	for _, item := range quantiles {
		serviceQ := fmt.Sprintf(
			`histogram_quantile(%g, sum by (le) (rate(core_service_request_duration_ms_bucket{service=%q}[%s])))`,
			item.q, service, window,
		)
		routeQ := fmt.Sprintf(
			`histogram_quantile(%g, sum by (le,route) (rate(core_service_request_duration_ms_bucket{service=%q}[%s])))`,
			item.q, service, window,
		)
		samples[serviceQ] = runtime.Vector{{Value: item.value}}
		samples[routeQ] = runtime.Vector{{
			Value:  item.value,
			Metric: map[string]string{"route": route},
		}}
	}

	agg := runtime.NewAggregator(fc, labeledProm{samples: samples}, runtime.Config{Mode: "prometheus"})
	detail, err := agg.ServiceDetail(context.Background(), window, service)

	require.NoError(t, err)
	require.NotNil(t, detail.Service.P95Ms.Value)
	require.Equal(t, 12.0, *detail.Service.P95Ms.Value)
	require.Len(t, detail.Routes, 1)
	require.Equal(t, 4.0, *detail.Routes[0].P50Ms.Value)
	require.Equal(t, 12.0, *detail.Routes[0].P95Ms.Value)
	require.Equal(t, 20.0, *detail.Routes[0].P99Ms.Value)
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
