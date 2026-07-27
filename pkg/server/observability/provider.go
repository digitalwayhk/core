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
	"depth":          {},
	"disk_bytes":     {},
	"sync_fail":      {},
	"oldest_age_sec": {},
	"publish_fail":   {},
	"lag":            {},
	"connections":    {},
	"queue_depth":    {},
	"hit_ratio":      {},
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
