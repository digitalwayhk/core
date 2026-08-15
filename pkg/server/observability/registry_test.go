// Package observability_test 验证组件 Provider 在同进程多服务场景下的注册与替换语义。
package observability_test

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestRegisterComponentProvidersReplacesSameComponent(t *testing.T) {
	observability.ResetComponentRegistryForTest()
	t.Cleanup(observability.ResetComponentRegistryForTest)

	require.NoError(t, observability.RegisterComponentProviders(
		"shop-order",
		&fakeProvider{name: "outbox", gauges: map[string]float64{"depth": 3}},
	))
	require.NoError(t, observability.RegisterComponentProviders(
		"shop-order",
		&fakeProvider{name: "outbox", gauges: map[string]float64{"depth": 0}},
	))

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	var depths []float64
	for _, family := range families {
		if family.GetName() != "core_component_gauge" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["service"] == "shop-order" &&
				labels["component"] == "outbox" &&
				labels["name"] == "depth" {
				depths = append(depths, metric.GetGauge().GetValue())
			}
		}
	}
	require.Equal(t, []float64{0}, depths)
}

func TestRegisterComponentProvidersKeepsDifferentServices(t *testing.T) {
	observability.ResetComponentRegistryForTest()
	t.Cleanup(observability.ResetComponentRegistryForTest)

	require.NoError(t, observability.RegisterComponentProviders(
		"shop-user",
		&fakeProvider{name: "pending", gauges: map[string]float64{"depth": 1}},
	))
	require.NoError(t, observability.RegisterComponentProviders(
		"shop-order",
		&fakeProvider{name: "outbox", gauges: map[string]float64{"depth": 2}},
	))

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	got := map[string]float64{}
	for _, family := range families {
		if family.GetName() != "core_component_gauge" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["name"] == "depth" {
				got[labels["service"]+"/"+labels["component"]] = metric.GetGauge().GetValue()
			}
		}
	}
	require.Equal(t, map[string]float64{
		"shop-user/pending": 1,
		"shop-order/outbox": 2,
	}, got)
}

var _ observability.RuntimeMetricProvider = (*fakeProvider)(nil)
