// Package event 提供 EventBridge 与 Outbox 的低基数运行时组件指标。
package event

import (
	"context"

	"github.com/digitalwayhk/core/pkg/server/observability"
)

// ComponentName 返回 EventBridge 组件名。
func (*ServiceEventBridge) ComponentName() string { return "eventbridge" }

// RuntimeMetricSnapshot 返回 EventBridge 队列、外部连接和发布失败快照。
func (b *ServiceEventBridge) RuntimeMetricSnapshot(context.Context) observability.RuntimeComponentSnapshot {
	if b == nil {
		return observability.RuntimeComponentSnapshot{Component: "eventbridge", State: "unavailable"}
	}
	queueDepth := len(b.observerQueue)
	for _, queue := range b.controlQueues {
		queueDepth += len(queue)
	}
	connections := float64(0)
	if b.HasExternalPublisher() {
		connections = 1
	}
	state := "ok"
	if b.closed.Load() {
		state = "unavailable"
	}
	return observability.RuntimeComponentSnapshot{
		Component: "eventbridge",
		State:     state,
		Gauges: map[string]float64{
			"queue_depth":  float64(queueDepth),
			"connections":  connections,
			"publish_fail": float64(b.publishFailures.Load() + b.dropped.Load() + b.controlQueueTimeouts.Load()),
		},
	}
}

// OutboxRuntimeMetricProvider 返回当前 Outbox 发布器指标 Provider；未启用时返回 nil。
func (b *ServiceEventBridge) OutboxRuntimeMetricProvider() observability.RuntimeMetricProvider {
	if b == nil {
		return nil
	}
	b.outboxMu.Lock()
	defer b.outboxMu.Unlock()
	if b.outbox == nil {
		return nil
	}
	return b.outbox
}

func (*outboxPublisher) ComponentName() string { return "outbox" }

func (p *outboxPublisher) RuntimeMetricSnapshot(context.Context) observability.RuntimeComponentSnapshot {
	if p == nil {
		return observability.RuntimeComponentSnapshot{Component: "outbox", State: "unavailable"}
	}
	state := "ok"
	if p.loadFailed.Load() {
		state = "unavailable"
	}
	return observability.RuntimeComponentSnapshot{
		Component: "outbox",
		State:     state,
		Gauges: map[string]float64{
			"depth":        float64(p.depth.Load()),
			"publish_fail": float64(p.failures.Load()),
		},
	}
}
