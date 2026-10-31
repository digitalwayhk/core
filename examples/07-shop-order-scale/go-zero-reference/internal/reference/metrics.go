// 本文件暴露原生进程指标及兼容现有压测采样格式的真实 pending 深度。
package reference

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (a *application) startMetrics() (*http.Server, error) {
	registry := prometheus.NewRegistry()
	registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}), prometheus.NewGoCollector())
	if a.local != nil {
		labels := prometheus.Labels{"component": "pending", "state": "ok", "name": "depth"}
		registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "core_component_gauge", ConstLabels: labels}, func() float64 { return float64(a.local.depth.Load()) }))
		registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "core_component_gauge", ConstLabels: prometheus.Labels{"component": "pending", "state": "ok", "name": "disk_bytes"}}, func() float64 { x, y := a.local.db.Size(); return float64(x + y) }))
	}
	if a.db != nil {
		pool, e := a.db.DB()
		if e != nil {
			return nil, e
		}
		registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "native_db_wait_seconds_total"}, func() float64 { return pool.Stats().WaitDuration.Seconds() }))
		registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "native_db_in_use"}, func() float64 { return float64(pool.Stats().InUse) }))
	}
	for phase, metric := range a.phases {
		registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "native_phase_seconds_total", ConstLabels: prometheus.Labels{"phase": phase}}, func() float64 { return float64(metric.nanos.Load()) / 1e9 }))
		registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "native_phase_attempts_total", ConstLabels: prometheus.Labels{"phase": phase}}, func() float64 { return float64(metric.attempts.Load()) }))
	}
	if a.db != nil {
		pool, err := a.db.DB()
		if err != nil {
			return nil, err
		}
		fields := map[string]func() float64{
			"wait_count_total": func() float64 { return float64(pool.Stats().WaitCount) }, "idle": func() float64 { return float64(pool.Stats().Idle) }, "open": func() float64 { return float64(pool.Stats().OpenConnections) },
			"idle_closed_total": func() float64 { return float64(pool.Stats().MaxIdleClosed) }, "idle_time_closed_total": func() float64 { return float64(pool.Stats().MaxIdleTimeClosed) }, "lifetime_closed_total": func() float64 { return float64(pool.Stats().MaxLifetimeClosed) },
		}
		for name, read := range fields {
			registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "native_db_" + name}, read))
		}
	}
	port := os.Getenv("SHOP_METRICS_PORT")
	listener, e := net.Listen("tcp", "127.0.0.1:"+port)
	if e != nil {
		return nil, e
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	s := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if e := s.Serve(listener); e != nil && e != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, e)
		}
	}()
	return s, nil
}
