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
