// 本文件验证正式ServiceContext的唯一预算、生命周期和真实Collector固定指标，不只采样helper。
package router_test

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestServiceContextOwnedWorkBudgetUniqueMetricsAndClose(t *testing.T) {
	name := fmt.Sprintf("work-budget-%d", time.Now().UnixNano())
	cfg := config.NewServiceDefaultConfig(name, 29993)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	sc := router.NewServiceContextWithConfig(&fakeService{name}, cfg)
	t.Cleanup(func() { sc.SetRunState(false); require.NoError(t, sc.ShutdownError()) })
	budget, err := sc.NewOwnedWorkBudget(ownership.GroupWorkBudgetOptions{Workers: 2, Prefetch: 4})
	require.NoError(t, err)
	_, err = sc.NewOwnedWorkBudget(ownership.GroupWorkBudgetOptions{Workers: 2, Prefetch: 4})
	require.ErrorIs(t, err, router.ErrResourceAlreadyRegistered)
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	seen := map[string]float64{}
	for _, f := range families {
		for _, metric := range f.Metric {
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["service"] != name || labels["component"] != "ownership_work" {
				continue
			}
			if f.GetName() == "core_component_gauge" {
				seen[labels["name"]] = metric.GetGauge().GetValue()
			} else if f.GetName() == "core_component_counter" {
				seen[labels["name"]] = metric.GetCounter().GetValue()
			}
		}
	}
	snapshot := budget.RuntimeMetricSnapshot(context.Background())
	for key, value := range snapshot.Gauges {
		got, ok := seen[key]
		require.True(t, ok, "缺少固定gauge %s", key)
		require.Equal(t, value, got)
	}
	for key, value := range snapshot.Counters {
		got, ok := seen[key]
		require.True(t, ok, "缺少固定counter %s", key)
		require.Equal(t, value, got)
	}
	require.Equal(t, 2.0, seen["ownership_work_workers"])
	require.Equal(t, 4.0, seen["ownership_work_prefetch_limit"])
	sc.SetRunState(false)
	require.NoError(t, sc.ShutdownError())
	select {
	case <-budget.Done():
	default:
		t.Fatal("ServiceContext没有关闭预算固定worker")
	}
}
func TestServiceContextOwnedWorkBudgetInitFailure(t *testing.T) {
	var nilSC *router.ServiceContext
	_, err := nilSC.NewOwnedWorkBudget(ownership.GroupWorkBudgetOptions{Workers: 1, Prefetch: 1})
	require.ErrorIs(t, err, router.ErrInvalidManagedResource)
	name := fmt.Sprintf("work-invalid-%d", time.Now().UnixNano())
	cfg := config.NewServiceDefaultConfig(name, 29994)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	sc := router.NewServiceContextWithConfig(&fakeService{name}, cfg)
	_, err = sc.NewOwnedWorkBudget(ownership.GroupWorkBudgetOptions{})
	require.Error(t, err)
	// 无效声明不能占住唯一资源；修正后仍可启动。
	b, err := sc.NewOwnedWorkBudget(ownership.GroupWorkBudgetOptions{Workers: 1, Prefetch: 1})
	require.NoError(t, err)
	sc.SetRunState(false)
	require.NoError(t, sc.ShutdownError())
	require.NoError(t, b.Close(context.Background()))
}
