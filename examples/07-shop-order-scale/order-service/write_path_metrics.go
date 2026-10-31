// 本文件复用Runtime组件接口暴露低基数写路径与已有MySQL连接池快照，不触发建连或GC。
package orderservice

import (
	"context"
	"database/sql"

	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/server/observability"
)

type writePathProvider struct {
	snapshot func() nosql.ReliableWriteMetrics
}

// ComponentName 返回固定低基数组件名。
func (p writePathProvider) ComponentName() string { return "write_path" }

// RuntimeMetricSnapshot 读取本地提交/同步累计耗时和已存在的MySQL池，不改变运行状态。
func (p writePathProvider) RuntimeMetricSnapshot(context.Context) observability.RuntimeComponentSnapshot {
	if p.snapshot == nil {
		return observability.RuntimeComponentSnapshot{Component: p.ComponentName(), State: "not_collected"}
	}
	m := p.snapshot()
	values := map[string]float64{
		"local_queue_wait_seconds_total": m.Batch.TotalQueueWaitDuration.Seconds(), "admission_seconds_total": m.Admission.TotalDuration.Seconds(), "admission_attempts_total": float64(m.Admission.Attempts),
		"admission_rejected_concurrency_total": float64(m.Admission.RejectedConcurrency),
		"admission_rejected_pending_total":     float64(m.Admission.RejectedPending),
		"admission_rejected_disk_total":        float64(m.Admission.RejectedDisk),
		"local_commit_seconds_total":           m.Batch.TotalCommitDuration.Seconds(), "local_committed_total": float64(m.Batch.Committed), "local_batches_total": float64(m.Batch.Batches),
		"sync_seconds_total": m.Sync.TotalDuration.Seconds(), "sync_attempts_total": float64(m.Sync.Attempts),
	}
	seen := map[*sql.DB]bool{}
	for _, connection := range oltp.GetConnectionManager().GetAllConnections() {
		if connection == nil || connection.DB == nil || connection.DB.Dialector.Name() != "mysql" {
			continue
		}
		pool, err := connection.DB.DB()
		if err != nil || seen[pool] {
			continue
		}
		seen[pool] = true
		s := pool.Stats()
		for k, v := range map[string]float64{"sql_wait_count_total": float64(s.WaitCount), "sql_wait_seconds_total": s.WaitDuration.Seconds(), "sql_open": float64(s.OpenConnections), "sql_in_use": float64(s.InUse), "sql_idle": float64(s.Idle), "sql_max_open": float64(s.MaxOpenConnections), "sql_idle_closed_total": float64(s.MaxIdleClosed), "sql_idle_time_closed_total": float64(s.MaxIdleTimeClosed), "sql_lifetime_closed_total": float64(s.MaxLifetimeClosed)} {
			values[k] += v
		}
	}
	return observability.RuntimeComponentSnapshot{Component: p.ComponentName(), State: "ok", Gauges: values}
}
