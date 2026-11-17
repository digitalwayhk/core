// 本文件验证真实归属快照经过正式Collector和HTTP处理器后不漏固定低基数指标，保留已知零值。
package ownership

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestRuntimeMetricsSurviveCollector固定名称和值必须从真实Runtime快照完整到达Collector；零值也是已采集的事实。
func TestRuntimeMetricsSurviveCollector(t *testing.T) {
	for _, populated := range []bool{false, true} {
		name := "known_zero"
		if populated {
			name = "populated"
		}
		t.Run(name, func(t *testing.T) {
			r := &Runtime{}
			gauges := map[string]float64{"ownership_active": 0, "ownership_ready": 0, "ownership_inflight": 0, "ownership_directory_keys": 0}
			counters := map[string]float64{"ownership_acquired_total": 0, "ownership_revoked_total": 0, "ownership_unknown_total": 0, "ownership_directory_errors_total": 0, "ownership_renew_failures_total": 0, "ownership_execution_revoked_total": 0, "ownership_observation_revoked_total": 0, "ownership_stalled_revoked_total": 0, "ownership_activation_failures_total": 0, "ownership_restore_failures_total": 0, "ownership_restore_seconds_total": 0}
			if populated {
				r.active.Store(3)
				r.ready.Store(2)
				r.inflight.Store(1)
				r.directoryKeys.Store(64)
				r.acquired.Store(8)
				r.revoked.Store(4)
				r.unknown.Store(5)
				r.directoryErrors.Store(6)
				r.renewFailures.Store(7)
				r.executionRevocations.Store(8)
				r.observationRevocations.Store(9)
				r.stalledRevocations.Store(10)
				r.activationFailures.Store(11)
				r.restoreFailures.Store(12)
				r.restoreNanos.Store(uint64(1500 * time.Millisecond))
				gauges = map[string]float64{"ownership_active": 3, "ownership_ready": 2, "ownership_inflight": 1, "ownership_directory_keys": 64}
				counters = map[string]float64{"ownership_acquired_total": 8, "ownership_revoked_total": 4, "ownership_unknown_total": 5, "ownership_directory_errors_total": 6, "ownership_renew_failures_total": 7, "ownership_execution_revoked_total": 8, "ownership_observation_revoked_total": 9, "ownership_stalled_revoked_total": 10, "ownership_activation_failures_total": 11, "ownership_restore_failures_total": 12, "ownership_restore_seconds_total": 1.5}
			}
			snapshot := r.RuntimeMetricSnapshot(context.Background())
			require.Equal(t, gauges, snapshot.Gauges)
			require.Equal(t, counters, snapshot.Counters)
			registry := prometheus.NewRegistry()
			require.NoError(t, registry.Register(observability.NewComponentCollector("fixture", []observability.RuntimeMetricProvider{r})))
			families, err := registry.Gather()
			require.NoError(t, err)
			actualGauges := map[string]float64{}
			actualCounters := map[string]float64{}
			for _, family := range families {
				for _, m := range family.GetMetric() {
					labels := map[string]string{}
					for _, label := range m.GetLabel() {
						labels[label.GetName()] = label.GetValue()
					}
					require.Len(t, labels, 4)
					require.Equal(t, "fixture", labels["service"])
					require.Equal(t, "ownership", labels["component"])
					require.Equal(t, "ok", labels["state"])
					switch family.GetName() {
					case "core_component_gauge":
						require.NotNil(t, m.Gauge)
						actualGauges[labels["name"]] = m.GetGauge().GetValue()
					case "core_component_counter":
						require.NotNil(t, m.Counter)
						actualCounters[labels["name"]] = m.GetCounter().GetValue()
					default:
						t.Fatalf("unexpected family %s", family.GetName())
					}
				}
			}
			require.Equal(t, gauges, actualGauges)
			require.Equal(t, counters, actualCounters)
			// 使用真实本地HTTP端口及正式promhttp处理器；不是现场抓取或业务归属验证。
			server := httptest.NewServer(promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
			defer server.Close()
			response, err := http.Get(server.URL)
			require.NoError(t, err)
			defer response.Body.Close()
			require.Equal(t, http.StatusOK, response.StatusCode)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			for _, values := range []map[string]float64{gauges, counters} {
				for metric := range values {
					require.Contains(t, string(body), `name="`+metric+`"`)
				}
			}
		})
	}
}

// TestUnavailableRuntimeMetricsDoNotBecomeZero不可用快照没有样本，补白名单不能将未采集伪装为已知零。
func TestUnavailableRuntimeMetricsDoNotBecomeZero(t *testing.T) {
	var runtime *Runtime
	snapshot := runtime.RuntimeMetricSnapshot(context.Background())
	require.Equal(t, "unavailable", snapshot.State)
	require.Nil(t, snapshot.Gauges)
	require.Nil(t, snapshot.Counters)
	registry := prometheus.NewRegistry()
	require.NoError(t, registry.Register(observability.NewComponentCollector("fixture", []observability.RuntimeMetricProvider{runtime})))
	families, err := registry.Gather()
	require.NoError(t, err)
	require.Empty(t, families)
}
