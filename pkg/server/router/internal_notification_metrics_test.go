// 本文件验证内部通知指标不伪造 Broker pending/retained，且仅使用固定低基数指标。
package router

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInternalNotificationMetricSnapshot(t *testing.T) {
	b := &internalNotificationBridge{kind: "cache", ready: true}
	b.RecordNotificationReconcile(true, time.Millisecond)
	b.RecordNotificationReconcile(false, 2*time.Millisecond)
	snapshot := b.RuntimeMetricSnapshot(context.Background())
	require.Equal(t, "internal-notification-cache", snapshot.Component)
	require.Equal(t, float64(1), snapshot.Gauges["connections"])
	require.Equal(t, float64(1), snapshot.Counters["notification_reconcile_total"])
	require.Equal(t, float64(1), snapshot.Counters["notification_reconcile_fail_total"])
	require.NotContains(t, snapshot.Gauges, "pending_messages")
	require.NotContains(t, snapshot.Gauges, "retained_messages")
}
