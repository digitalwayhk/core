package observability

import "context"

// RuntimeComponentSnapshot 是组件运行时指标的只读快照。
type RuntimeComponentSnapshot struct {
	Component string
	State     string // ok|not_collected|unavailable
	Gauges    map[string]float64
	Counters  map[string]float64
}

// RuntimeMetricProvider 由本进程组件实现；只用于注册 Collector，不由 Aggregator 远程调用。
type RuntimeMetricProvider interface {
	ComponentName() string
	RuntimeMetricSnapshot(ctx context.Context) RuntimeComponentSnapshot
}

// allowedGaugeNames 组件 gauge 名白名单（低基数）。
var allowedGaugeNames = map[string]struct{}{
	"depth":             {},
	"disk_bytes":        {},
	"sync_fail":         {},
	"oldest_age_sec":    {},
	"publish_fail":      {},
	"lag":               {},
	"connections":       {},
	"queue_depth":       {},
	"hit_ratio":         {},
	"key_concurrency":   {},
	"worker_inflight":   {},
	"worker_peak":       {},
	"active_lanes":      {},
	"blocked_keys":      {},
	"batch_size":        {},
	"batch_limit":       {},
	"handler_inflight":  {},
	"handler_peak":      {},
	"active_keys":       {},
	"pending_keys":      {},
	"retained_messages": {},
	"retained_bytes":    {},
	"backlog_messages":  {},
	"pending_messages":  {},
}

var allowedCounterNames = map[string]struct{}{
	"reclaimed_total":        {},
	"reclaim_fail_total":     {},
	"redelivered_total":      {},
	"dead_letter_total":      {},
	"dead_letter_fail_total": {},
	"publish_rejected_total": {},
}

func filterGauges(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for k, v := range in {
		if _, ok := allowedGaugeNames[k]; ok {
			out[k] = v
		}
	}
	return out
}

func filterCounters(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for key, value := range in {
		if _, ok := allowedCounterNames[key]; ok {
			out[key] = value
		}
	}
	return out
}
