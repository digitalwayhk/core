package observability_test

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestRecordCallIncrementsCounter(t *testing.T) {
	observability.EnableMetrics()

	labels := map[string]string{
		"source_service": "shop-user",
		"target_service": "shop-order",
		"target_route":   "/api/shop-order/addorder",
		"protocol":       "grpc",
		"result_class":   "success",
	}
	before := gatherCounter(t, "core_service_call_requests_total", labels)

	observability.RecordCall(observability.CallLabels{
		SourceService: "shop-user",
		TargetService: "shop-order",
		TargetRoute:   "/api/shop-order/addorder",
		Protocol:      "grpc",
		ResultClass:   observability.ResultSuccess,
	}, 12*time.Millisecond)

	after := gatherCounter(t, "core_service_call_requests_total", labels)
	require.Equal(t, before+1, after)
}

func TestRecordCallRejectsHighCardinalityRoute(t *testing.T) {
	observability.EnableMetrics()

	labels := map[string]string{
		"source_service": "shop-user",
		"target_service": "shop-order",
		"target_route":   "invalid_route",
		"protocol":       "grpc",
		"result_class":   "success",
	}
	before := gatherCounter(t, "core_service_call_requests_total", labels)

	observability.RecordCall(observability.CallLabels{
		SourceService: "shop-user",
		TargetService: "shop-order",
		TargetRoute:   "/api/x?id=1",
		Protocol:      "grpc",
		ResultClass:   observability.ResultSuccess,
	}, time.Millisecond)

	after := gatherCounter(t, "core_service_call_requests_total", labels)
	require.Equal(t, before+1, after)
}

func TestRecordInboundRequestIncrementsCounter(t *testing.T) {
	observability.EnableMetrics()

	labels := map[string]string{
		"service":      "shop-order",
		"route":        "/api/shop-order/addorder",
		"protocol":     "grpc",
		"result_class": "success",
	}
	before := gatherCounter(t, "core_service_request_requests_total", labels)

	observability.RecordInboundRequest(
		"shop-order",
		"/api/shop-order/addorder",
		"grpc",
		observability.ResultSuccess,
		5*time.Millisecond,
	)

	after := gatherCounter(t, "core_service_request_requests_total", labels)
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
	if len(got) < len(want) {
		return false
	}
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
