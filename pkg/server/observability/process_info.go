package observability

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	processInfoOnce sync.Once
	processInfo     *prometheus.GaugeVec
)

// ensureProcessInfoMetric 暴露进程身份 gauge，使 scrape 样本自带 service/service_instance_id。
func ensureProcessInfoMetric() {
	processInfoOnce.Do(func() {
		processInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "core",
			Subsystem: "process",
			Name:      "info",
			Help:      "process identity labels (always 1 when registered)",
		}, []string{"service", "service_instance_id"})
		_ = prometheus.Register(processInfo)
	})
}

// attachProcessInfoGauge 在进程标签就绪后写入 info=1。
func attachProcessInfoGauge(service, instanceID string) {
	ensureProcessInfoMetric()
	if processInfo == nil {
		return
	}
	processInfo.Reset()
	processInfo.WithLabelValues(service, instanceID).Set(1)
}
