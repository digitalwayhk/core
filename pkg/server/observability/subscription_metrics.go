package observability

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	subMetricOnce sync.Once
	subInfo       *prometheus.GaugeVec
)

func ensureSubscriptionMetric() {
	subMetricOnce.Do(func() {
		subInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "core",
			Subsystem: "event",
			Name:      "subscription_info",
			Help:      "registered business event subscriptions (1=active)",
		}, []string{"target_service", "subject_family", "event_type", "reliable"})
		_ = prometheus.Register(subInfo)
	})
}

// SetSubscriptionActive 导出跨进程可见的订阅事实（Aggregator 通过 Prom 查询）。
func SetSubscriptionActive(targetService, subjectFamily, eventType string, reliable, active bool) {
	ensureSubscriptionMetric()
	if subInfo == nil {
		return
	}
	target := NormalizeServiceLabel(targetService)
	family := normalizeSubjectFamily(subjectFamily)
	et := NormalizeServiceLabel(eventType)
	if et == "unknown" {
		et = "unspecified"
	}
	if target == "unknown" || family == "" {
		return
	}
	rel := "false"
	if reliable {
		rel = "true"
	}
	if active {
		subInfo.WithLabelValues(target, family, et, rel).Set(1)
		return
	}
	subInfo.DeleteLabelValues(target, family, et, rel)
}
