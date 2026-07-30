// Package router 验证 ServiceContext 自动注册 EventBridge 与 Outbox 运行时组件指标。
package router

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type runtimeMetricOutboxStore struct{}

func (runtimeMetricOutboxStore) LoadPending(context.Context, int) ([]event.OutboxMessage, error) {
	return nil, nil
}

func (runtimeMetricOutboxStore) MarkPublished(context.Context, event.OutboxMessage) error {
	return nil
}

func TestServiceContextRegistersEventRuntimeProviders(t *testing.T) {
	observability.ResetComponentRegistryForTest()
	t.Cleanup(observability.ResetComponentRegistryForTest)

	serviceName := fmt.Sprintf("runtime-metrics-%d", time.Now().UnixNano())
	cfg := config.NewServiceDefaultConfig(serviceName, 0)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	sc := NewServiceContextWithConfig(&metricsTestService{name: serviceName}, cfg)
	require.NotNil(t, sc)
	require.NoError(t, sc.UseOutbox(runtimeMetricOutboxStore{}))

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	components := map[string]bool{}
	for _, family := range families {
		if family.GetName() != "core_component_gauge" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["service"] == serviceName {
				components[labels["component"]] = true
			}
		}
	}
	require.True(t, components["eventbridge"])
	require.True(t, components["outbox"])
}
