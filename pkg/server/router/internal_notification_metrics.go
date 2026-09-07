// 本文件仅采集进程内可证明的通知指标，不将瞬时广播伪装为持久化队列。
package router

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/zeromicro/go-zero/core/logx"
)

type internalNotificationMetrics struct {
	published, publishFailed, gaps              atomic.Int64
	reconciled, reconcileFailed, reconcileNanos atomic.Int64
}

func (b *internalNotificationBridge) ComponentName() string { return "internal-notification-" + b.kind }

func (sc *ServiceContext) registerInternalNotificationMetrics(b *internalNotificationBridge) {
	if err := sc.RegisterRuntimeMetricProviders(b); err != nil {
		logx.Infow("runtime_metric_provider_register_failed", logx.Field("service", sc.Service.Name), logx.Field("component", b.ComponentName()), logx.Field("error", err))
	}
}

func (b *internalNotificationBridge) RecordNotificationReconcile(success bool, elapsed time.Duration) {
	if success {
		b.metrics.reconciled.Add(1)
	} else {
		b.metrics.reconcileFailed.Add(1)
	}
	b.metrics.reconcileNanos.Store(int64(elapsed))
}

func (b *internalNotificationBridge) RuntimeMetricSnapshot(context.Context) observability.RuntimeComponentSnapshot {
	_, ready := b.NotificationState()
	s := observability.RuntimeComponentSnapshot{Component: b.ComponentName(), State: "unavailable",
		Gauges: map[string]float64{"connections": 0}, Counters: map[string]float64{
			"notification_published_total": float64(b.metrics.published.Load()),
			"publish_rejected_total":       float64(b.metrics.publishFailed.Load()),
			"notification_gap_total":       float64(b.metrics.gaps.Load()),
		}}
	if b.kind == "cache" {
		s.Counters["notification_reconcile_total"] = float64(b.metrics.reconciled.Load())
		s.Counters["notification_reconcile_fail_total"] = float64(b.metrics.reconcileFailed.Load())
	}
	if ready {
		s.State = "ok"
		s.Gauges["connections"] = 1
	}
	if b.metrics.reconciled.Load()+b.metrics.reconcileFailed.Load() > 0 {
		s.Gauges["notification_reconcile_seconds"] = time.Duration(b.metrics.reconcileNanos.Load()).Seconds()
	}
	return s
}
