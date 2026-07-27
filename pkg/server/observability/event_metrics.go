package observability

import (
	"sync"

	"github.com/zeromicro/go-zero/core/metric"
)

var (
	eventOnce    sync.Once
	eventPublish metric.CounterVec
)

func initEventMetrics() {
	eventOnce.Do(func() {
		eventPublish = metric.NewCounterVec(&metric.CounterVecOpts{
			Namespace: "core",
			Subsystem: "event",
			Name:      "publish_total",
			Help:      "core event publish results by subject family",
			Labels:    []string{"source_service", "subject_family", "event_type", "result_class"},
		})
	})
}

// RecordEventPublish 记录事件发布（subject 会归一化为 family）。
func RecordEventPublish(sourceService, subject, eventType, resultClass string) {
	initEventMetrics()
	EnableMetrics()
	src := NormalizeServiceLabel(sourceService)
	family := normalizeSubjectFamily(subject)
	et := NormalizeServiceLabel(eventType)
	if et == "unknown" {
		et = "unspecified"
	}
	rc := NormalizeResultClass(resultClass)
	if eventPublish != nil && family != "" {
		eventPublish.Inc(src, family, et, rc)
	}
}

func normalizeSubjectFamily(subject string) string {
	s := NormalizeServiceLabel(subject)
	if s == "unknown" {
		return ""
	}
	if len(s) > 64 {
		return s[:64]
	}
	return s
}
