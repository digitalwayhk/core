// 本文件导出实例预算一次性固定指标，不能按每scope重复汇总同一预算。
package ownership

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/observability"
	"time"
)

// ComponentName标识实例级工作预算，与原ownership租约组件分开。
func (*GroupWorkBudget) ComponentName() string { return "ownership_work" }

// RuntimeMetricSnapshot只输出固定资源和累计时钟；不记录group、凭据或业务身份。
func (b *GroupWorkBudget) RuntimeMetricSnapshot(context.Context) observability.RuntimeComponentSnapshot {
	if b == nil {
		return observability.RuntimeComponentSnapshot{Component: "ownership_work", State: "unavailable"}
	}
	return observability.RuntimeComponentSnapshot{
		Component: "ownership_work", State: "ok",
		Gauges: map[string]float64{
			"ownership_work_workers":        float64(b.options.Workers),
			"ownership_work_prefetch_limit": float64(b.options.Prefetch),
			"ownership_work_active":         float64(b.active.Load()),
			"ownership_work_reserved":       float64(b.reserved.Load()),
			"ownership_work_queued":         float64(b.queued.Load()),
		},
		Counters: map[string]float64{
			"ownership_work_started_total":            float64(b.started.Load()),
			"ownership_work_completed_total":          float64(b.completed.Load()),
			"ownership_work_errors_total":             float64(b.failed.Load()),
			"ownership_work_no_progress_total":        float64(b.noProgress.Load()),
			"ownership_work_selections_total":         float64(b.selections.Load()),
			"ownership_work_selection_errors_total":   float64(b.selectionErrors.Load()),
			"ownership_work_timeouts_total":           float64(b.timeouts.Load()),
			"ownership_work_capacity_wait_total":      float64(b.capacityWaits.Load()),
			"ownership_work_queue_wait_seconds_total": float64(b.queueNanos.Load()) / float64(time.Second),
			"ownership_work_selection_seconds_total":  float64(b.selectionNanos.Load()) / float64(time.Second),
			"ownership_work_handler_seconds_total":    float64(b.handlerNanos.Load()) / float64(time.Second),
		},
	}
}
