package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

type metricsPingRoute struct {
	info *types.RouterInfo
}

func (m *metricsPingRoute) Parse(types.IRequest) error             { return nil }
func (m *metricsPingRoute) Validation(types.IRequest) error        { return nil }
func (m *metricsPingRoute) Do(types.IRequest) (interface{}, error) { return "pong", nil }
func (m *metricsPingRoute) RouterInfo() *types.RouterInfo          { return m.info }

type metricsTestService struct {
	name    string
	routers []types.IRouter
}

func (s *metricsTestService) ServiceName() string      { return s.name }
func (s *metricsTestService) Routers() []types.IRouter { return s.routers }

func TestCallServiceRecordsEdgeMetrics(t *testing.T) {
	observability.EnableMetrics()
	observability.ResetProcessLabelsForTest()
	t.Cleanup(observability.ResetProcessLabelsForTest)

	targetName := fmt.Sprintf("demo-b-metrics-%d", time.Now().UnixNano())
	sourceName := fmt.Sprintf("demo-a-metrics-%d", time.Now().UnixNano())
	path := "/api/" + targetName + "/ping"

	route := &metricsPingRoute{}
	info := &types.RouterInfo{
		Path:        path,
		ServiceName: targetName,
		Method:      http.MethodPost,
		PathType:    types.PublicType,
	}
	route.info = info
	info.SetInstance(route)

	cfg := config.NewServiceDefaultConfig(targetName, 0)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	target := NewServiceContextWithConfig(&metricsTestService{
		name:    targetName,
		routers: []types.IRouter{route},
	}, cfg)
	require.NotNil(t, target)
	t.Cleanup(func() { target.SetRunState(false) })
	require.Same(t, target, GetContext(targetName))

	sourceCfg := config.NewServiceDefaultConfig(sourceName, 0)
	sourceCfg.Cluster.Mode = "off"
	sourceCfg.MQ.Mode = "off"
	source := NewServiceContextWithConfig(&metricsTestService{name: sourceName}, sourceCfg)
	require.NotNil(t, source)
	t.Cleanup(func() { source.SetRunState(false) })

	labels := map[string]string{
		"source_service": sourceName,
		"target_service": targetName,
		"target_route":   path,
		"protocol":       "local",
		"result_class":   "success",
	}
	before := gatherCounter(t, "core_service_call_requests_total", labels)

	resp, err := source.CallService(&types.PayLoad{
		TargetService: targetName,
		TargetPath:    path,
		HttpMethod:    http.MethodPost,
		Instance:      json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	after := gatherCounter(t, "core_service_call_requests_total", labels)
	require.Equal(t, before+1, after)
}

func TestCallServiceRecordsUnavailable(t *testing.T) {
	observability.EnableMetrics()

	sourceName := fmt.Sprintf("demo-a-unavail-%d", time.Now().UnixNano())
	cfg := config.NewServiceDefaultConfig(sourceName, 0)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	source := NewServiceContextWithConfig(&metricsTestService{name: sourceName}, cfg)
	require.NotNil(t, source)
	t.Cleanup(func() { source.SetRunState(false) })

	labels := map[string]string{
		"source_service": sourceName,
		"target_service": "missing-service",
		"target_route":   "/api/x/y",
		"protocol":       "grpc",
		"result_class":   "unavailable",
	}
	before := gatherCounter(t, "core_service_call_requests_total", labels)

	_, err := source.CallService(&types.PayLoad{
		TargetService: "missing-service",
		TargetPath:    "/api/x/y",
	})
	require.Error(t, err)

	after := gatherCounter(t, "core_service_call_requests_total", labels)
	require.Equal(t, before+1, after)
}

func gatherCounter(t *testing.T, name string, want map[string]string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if matchLabels(m.GetLabel(), want) {
				if m.GetCounter() != nil {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func matchLabels(got []*dto.LabelPair, want map[string]string) bool {
	values := make(map[string]string, len(got))
	for _, l := range got {
		values[l.GetName()] = l.GetValue()
	}
	for k, v := range want {
		if values[k] != v {
			return false
		}
	}
	return true
}
