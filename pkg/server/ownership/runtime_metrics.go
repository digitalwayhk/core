// 本文件导出低基数归属汇总，不以业务key/用户/订单作指标标签。
package ownership

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/observability"
)

// ComponentName 标识框架归属组件。
func (*Runtime) ComponentName() string { return "ownership" }

// RuntimeMetricSnapshot 只提供当前实例有限worker和归属变化计数。
func (r *Runtime) RuntimeMetricSnapshot(context.Context) observability.RuntimeComponentSnapshot {
	if r == nil {
		return observability.RuntimeComponentSnapshot{Component: "ownership", State: "unavailable"}
	}
	return observability.RuntimeComponentSnapshot{Component: "ownership", State: "ok", Gauges: map[string]float64{"ownership_active": float64(r.active.Load()), "ownership_ready": float64(r.ready.Load()), "ownership_inflight": float64(r.inflight.Load())}, Counters: map[string]float64{"ownership_acquired_total": float64(r.acquired.Load()), "ownership_revoked_total": float64(r.revoked.Load()), "ownership_unknown_total": float64(r.unknown.Load())}}
}
