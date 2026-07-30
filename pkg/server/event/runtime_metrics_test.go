// Package event_test 验证 EventBridge 与 Outbox 启用后立即提供低基数组件指标。
package event_test

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/require"
)

type runtimeMetricsOutboxStore struct{}

func (runtimeMetricsOutboxStore) LoadPending(context.Context, int) ([]event.OutboxMessage, error) {
	return nil, nil
}

func (runtimeMetricsOutboxStore) MarkPublished(context.Context, event.OutboxMessage) error {
	return nil
}

func TestServiceEventBridgeRuntimeMetricsExistWithoutTraffic(t *testing.T) {
	bridge := event.NewServiceEventBridge(nil, event.ServiceEventBridgeOptions{})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, bridge.Close(ctx))
	})

	snapshot := bridge.RuntimeMetricSnapshot(context.Background())

	require.Equal(t, "eventbridge", snapshot.Component)
	require.Equal(t, "ok", snapshot.State)
	require.Equal(t, float64(0), snapshot.Gauges["queue_depth"])
	require.Equal(t, float64(0), snapshot.Gauges["connections"])
	require.Equal(t, float64(0), snapshot.Gauges["publish_fail"])
}

func TestOutboxRuntimeMetricsExistWithoutBacklog(t *testing.T) {
	bridge := event.NewServiceEventBridge(nil, event.ServiceEventBridgeOptions{})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, bridge.Close(ctx))
	})
	require.NoError(t, bridge.UseOutbox(event.OutboxOptions{
		SourceService: "shop-order",
		Store:         runtimeMetricsOutboxStore{},
		Interval:      time.Hour,
	}))

	provider := bridge.OutboxRuntimeMetricProvider()
	require.NotNil(t, provider)
	snapshot := provider.RuntimeMetricSnapshot(context.Background())

	require.Equal(t, "outbox", snapshot.Component)
	require.Equal(t, "ok", snapshot.State)
	require.Equal(t, float64(0), snapshot.Gauges["depth"])
	require.Equal(t, float64(0), snapshot.Gauges["publish_fail"])
}
