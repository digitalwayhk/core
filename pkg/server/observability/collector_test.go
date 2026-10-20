package observability_test

import (
	"context"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	name     string
	gauges   map[string]float64
	counters map[string]float64
}

func (p *fakeProvider) ComponentName() string { return p.name }
func (p *fakeProvider) RuntimeMetricSnapshot(context.Context) observability.RuntimeComponentSnapshot {
	return observability.RuntimeComponentSnapshot{
		Component: p.name,
		State:     "ok",
		Gauges:    p.gauges,
		Counters:  p.counters,
	}
}

func TestCollectorExportsWhitelistedProviderCounters(t *testing.T) {
	reg := prometheus.NewRegistry()
	p := &fakeProvider{name: "mq", counters: map[string]float64{"reclaimed_total": 3, "message_id_123": 9}}
	c := observability.NewComponentCollector("shop-order", []observability.RuntimeMetricProvider{p})
	require.NoError(t, reg.Register(c))

	mfs, err := reg.Gather()
	require.NoError(t, err)
	found := false
	for _, mf := range mfs {
		if mf.GetName() != "core_component_counter" {
			continue
		}
		for _, metric := range mf.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["name"] == "reclaimed_total" {
				found = true
				require.Equal(t, 3.0, metric.GetCounter().GetValue())
			}
			require.NotEqual(t, "message_id_123", labels["name"])
		}
	}
	require.True(t, found)
}

func TestCollectorExportsProviderGauges(t *testing.T) {
	reg := prometheus.NewRegistry()
	p := &fakeProvider{name: "pending", gauges: map[string]float64{"depth": 3, "evil_sql": 9}}
	c := observability.NewComponentCollector("shop-order", []observability.RuntimeMetricProvider{p})
	require.NoError(t, reg.Register(c))

	mfs, err := reg.Gather()
	require.NoError(t, err)
	var depth float64
	found := false
	for _, mf := range mfs {
		if mf.GetName() != "core_component_gauge" {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["component"] == "pending" && labels["name"] == "depth" {
				found = true
				depth = m.GetGauge().GetValue()
			}
			require.NotEqual(t, "evil_sql", labels["name"])
		}
	}
	require.True(t, found)
	require.Equal(t, 3.0, depth)
}

// TestCollectorExportsAdmissionRejections 验证三类背压事实可观测，同时继续过滤高基数名称。
func TestCollectorExportsAdmissionRejections(t *testing.T) {
	expected := map[string]float64{
		"admission_rejected_concurrency_total": 2,
		"admission_rejected_pending_total":     3,
		"admission_rejected_disk_total":        4,
	}
	values := map[string]float64{"request_id_123": 99}
	for name, value := range expected {
		values[name] = value
	}
	registry := prometheus.NewRegistry()
	require.NoError(t, registry.Register(observability.NewComponentCollector("shop-order", []observability.RuntimeMetricProvider{
		&fakeProvider{name: "write_path", gauges: values},
	})))
	families, err := registry.Gather()
	require.NoError(t, err)
	actual := map[string]float64{}
	for _, family := range families {
		if family.GetName() != "core_component_gauge" {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "name" {
					actual[label.GetValue()] = metric.GetGauge().GetValue()
				}
			}
		}
	}
	require.Equal(t, expected, actual)
}

func TestCollectorExportsReliableQueueAndClocks(t *testing.T) {
	gauges := map[string]float64{"prefetch_capacity": 8, "prefetched_messages": 3, "queued_work": 1}
	counters := map[string]float64{
		"reliable_new_read_total": 2, "reliable_reclaim_read_total": 3,
		"reliable_publish_to_read_seconds_total": 0.2, "reliable_schedule_wait_seconds_total": 0.1,
		"reliable_handler_seconds_total": 0.01, "reliable_pel_wait_seconds_total": 0.03,
		"reliable_pel_wait_samples_total": 2, "reliable_scheduled_total": 2, "reliable_handler_calls_total": 2,
	}
	registry := prometheus.NewRegistry()
	require.NoError(t, registry.Register(observability.NewComponentCollector("service", []observability.RuntimeMetricProvider{
		&fakeProvider{name: "mq", gauges: gauges, counters: counters},
	})))
	families, err := registry.Gather()
	require.NoError(t, err)
	actualGauges, actualCounters := map[string]float64{}, map[string]float64{}
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "name" {
					if metric.Counter != nil {
						actualCounters[label.GetValue()] = metric.GetCounter().GetValue()
					} else if metric.Gauge != nil {
						actualGauges[label.GetValue()] = metric.GetGauge().GetValue()
					}
				}
			}
		}
	}
	require.Equal(t, gauges, actualGauges)
	require.Equal(t, counters, actualCounters)
}
