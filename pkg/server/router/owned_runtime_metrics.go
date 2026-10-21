// 本文件汇总同一服务实例的有限归属运行时，避免同名Prometheus组件相互覆盖。
package router

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"sync"
)

type ownedRuntimeMetrics struct {
	mu       sync.RWMutex
	runtimes []*ownership.Runtime
}

// ComponentName 使用固定组件名，不引入scope或用户指标标签。
func (*ownedRuntimeMetrics) ComponentName() string { return "ownership" }
func (m *ownedRuntimeMetrics) size() int           { m.mu.RLock(); defer m.mu.RUnlock(); return len(m.runtimes) }
func (m *ownedRuntimeMetrics) add(r *ownership.Runtime) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runtimes = append(m.runtimes, r)
}

// RuntimeMetricSnapshot 汇总本实例有限运行时，保留累计计数与实际在途数。
func (m *ownedRuntimeMetrics) RuntimeMetricSnapshot(ctx context.Context) observability.RuntimeComponentSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := observability.RuntimeComponentSnapshot{Component: "ownership", State: "ok", Gauges: map[string]float64{}, Counters: map[string]float64{}}
	for _, r := range m.runtimes {
		snapshot := r.RuntimeMetricSnapshot(ctx)
		for name, value := range snapshot.Gauges {
			result.Gauges[name] += value
		}
		for name, value := range snapshot.Counters {
			result.Counters[name] += value
		}
	}
	return result
}
