package observability

import (
	"context"
)

// ReliableWriteMetricsSnapshot 是 Pending/可靠写指标的最小快照，避免 observability 依赖 persistence 包循环。
type ReliableWriteMetricsSnapshot struct {
	Pending   int
	DiskBytes int64
	SyncFail  float64
}

// ReliableWriteProvider 将可靠写 Metrics 暴露为 RuntimeMetricProvider。
type ReliableWriteProvider struct {
	// Snapshot 由业务/store 注入；必须只读且低开销。
	Snapshot func() ReliableWriteMetricsSnapshot
}

// ComponentName 返回组件名。
func (p ReliableWriteProvider) ComponentName() string { return "pending" }

// RuntimeMetricSnapshot 读取当前可靠写快照。
func (p ReliableWriteProvider) RuntimeMetricSnapshot(context.Context) RuntimeComponentSnapshot {
	if p.Snapshot == nil {
		return RuntimeComponentSnapshot{Component: "pending", State: "not_collected"}
	}
	m := p.Snapshot()
	return RuntimeComponentSnapshot{
		Component: "pending",
		State:     "ok",
		Gauges: map[string]float64{
			"depth":      float64(m.Pending),
			"disk_bytes": float64(m.DiskBytes),
			"sync_fail":  m.SyncFail,
		},
	}
}
