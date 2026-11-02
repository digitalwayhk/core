// 本文件导出低基数归属汇总，不以业务key/用户/订单作指标标签。
package ownership

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/observability"
	"time"
)

// ComponentName 标识框架归属组件。
func (*Runtime) ComponentName() string { return "ownership" }

// RuntimeMetricSnapshot 只提供当前实例有限worker和归属变化计数。
func (r *Runtime) RuntimeMetricSnapshot(context.Context) observability.RuntimeComponentSnapshot {
	if r == nil {
		return observability.RuntimeComponentSnapshot{Component: "ownership", State: "unavailable"}
	}
	return observability.RuntimeComponentSnapshot{Component: "ownership", State: "ok", Gauges: map[string]float64{"ownership_active": float64(r.active.Load()), "ownership_ready": float64(r.ready.Load()), "ownership_inflight": float64(r.inflight.Load()), "ownership_directory_keys": float64(r.directoryKeys.Load())}, Counters: map[string]float64{"ownership_acquired_total": float64(r.acquired.Load()), "ownership_revoked_total": float64(r.revoked.Load()), "ownership_unknown_total": float64(r.unknown.Load()), "ownership_directory_errors_total": float64(r.directoryErrors.Load()),
		"ownership_renew_failures_total":      float64(r.renewFailures.Load()),
		"ownership_execution_revoked_total":   float64(r.executionRevocations.Load()),
		"ownership_observation_revoked_total": float64(r.observationRevocations.Load()),
		"ownership_stalled_revoked_total":     float64(r.stalledRevocations.Load()),
		"ownership_activation_failures_total": float64(r.activationFailures.Load()),
		"ownership_restore_failures_total":    float64(r.restoreFailures.Load()),
		"ownership_restore_seconds_total":     float64(r.restoreNanos.Load()) / float64(time.Second)}}
}
