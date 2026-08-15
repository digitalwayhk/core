package observability

import (
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/metric"
	"github.com/zeromicro/go-zero/core/prometheus"
)

var (
	metricsOnce sync.Once

	callRequests metric.CounterVec
	callDuration metric.HistogramVec

	requestTotal    metric.CounterVec
	requestDuration metric.HistogramVec
)

func initMetrics() {
	metricsOnce.Do(func() {
		callRequests = metric.NewCounterVec(&metric.CounterVecOpts{
			Namespace: "core",
			Subsystem: "service_call",
			Name:      "requests_total",
			Help:      "core inter-service call results",
			Labels:    []string{"source_service", "target_service", "target_route", "protocol", "result_class"},
		})
		callDuration = metric.NewHistogramVec(&metric.HistogramVecOpts{
			Namespace: "core",
			Subsystem: "service_call",
			Name:      "duration_ms",
			Help:      "core inter-service call duration(ms)",
			Labels:    []string{"source_service", "target_service", "target_route", "protocol"},
			Buckets:   []float64{1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 5000},
		})
		requestTotal = metric.NewCounterVec(&metric.CounterVecOpts{
			Namespace: "core",
			Subsystem: "service_request",
			Name:      "requests_total",
			Help:      "core inbound requests by stable route",
			Labels:    []string{"service", "route", "protocol", "result_class"},
		})
		requestDuration = metric.NewHistogramVec(&metric.HistogramVecOpts{
			Namespace: "core",
			Subsystem: "service_request",
			Name:      "duration_ms",
			Help:      "core inbound request duration(ms)",
			Labels:    []string{"service", "route", "protocol"},
			Buckets:   []float64{1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 5000},
		})
	})
}

// EnableMetrics 打开 go-zero prometheus 记录门闩（测试或未走 StartAgent 时调用）。
func EnableMetrics() {
	prometheus.Enable()
	initMetrics()
}

// CallLabels 描述一次跨服务调用边。
type CallLabels struct {
	SourceService string
	TargetService string
	TargetRoute   string
	Protocol      string
	ResultClass   string
}

// RecordCall 记录跨服务调用边计数与耗时（ms）。
func RecordCall(l CallLabels, d time.Duration) {
	initMetrics()
	src := NormalizeServiceLabel(l.SourceService)
	tgt := NormalizeServiceLabel(l.TargetService)
	route := NormalizeRouteLabel(l.TargetRoute)
	proto := NormalizeProtocol(l.Protocol)
	result := NormalizeResultClass(l.ResultClass)
	if callRequests != nil {
		callRequests.Inc(src, tgt, route, proto, result)
	}
	if callDuration != nil {
		ms := d.Milliseconds()
		if ms < 0 {
			ms = 0
		}
		callDuration.Observe(ms, src, tgt, route, proto)
	}
}

// RecordInboundRequest 记录入站请求（稳定路由模板维度）。
func RecordInboundRequest(service, route, protocol, result string, d time.Duration) {
	initMetrics()
	svc := NormalizeServiceLabel(service)
	r := NormalizeRouteLabel(route)
	p := NormalizeProtocol(protocol)
	rc := NormalizeResultClass(result)
	if requestTotal != nil {
		requestTotal.Inc(svc, r, p, rc)
	}
	if requestDuration != nil {
		ms := d.Milliseconds()
		if ms < 0 {
			ms = 0
		}
		requestDuration.Observe(ms, svc, r, p)
	}
}
