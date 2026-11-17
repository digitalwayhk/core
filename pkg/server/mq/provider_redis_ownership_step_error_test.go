// 本文件验证完整Runtime中的真实Step错误不会被BlockedDue声明屏蔽，不只手动设置监视器标志。
package mq

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
)

// TestRedisOwnershipBlockedDuePersistentStepErrorStalls真实回调反复出错时仍沿原stall撤销。
func TestRedisOwnershipBlockedDuePersistentStepErrorStalls(t *testing.T) {
	p := newPoolBudgetProvider(t, 4, 4)
	ctx := context.Background()
	m := ownership.Manifest{Scope: "blocked-step-error", Service: "fixture", Group: "fixture", Selector: "opaque-v1", Keys: []string{"A"}, Subjects: []string{"input"}, Timing: ownership.Timing{Lease: time.Second, Renew: 30 * time.Millisecond, Poll: 10 * time.Millisecond, Execution: 200 * time.Millisecond, Stall: 500 * time.Millisecond, Observation: 150 * time.Millisecond}}.Normalize()
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	store, err := p.OwnershipStore(ctx)
	require.NoError(t, err)
	var calls atomic.Int32
	runtime, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "boot", MaxOwned: 1, Activate: func(context.Context, ownership.Credential) error { return nil }, Step: func(context.Context, ownership.Credential) (bool, error) {
		calls.Add(1)
		return false, errors.New("synthetic step failure")
	}, Observe: func(context.Context, ownership.Credential) (ownership.Observation, error) {
		return ownership.Observation{At: time.Now(), Due: 1, BlockedDue: 1}, nil
	}})
	require.NoError(t, err)
	defer runtime.Close(ctx)
	require.Eventually(t, func() bool {
		return calls.Load() > 0 && runtime.RuntimeMetricSnapshot(ctx).Gauges["ownership_ready"] == 1
	}, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return runtime.RuntimeMetricSnapshot(ctx).Counters["ownership_stalled_revoked_total"] > 0 }, 2*time.Second, time.Millisecond, "Step错误不能仅靠业务阻挡分类无限保持归属")
	require.Greater(t, calls.Load(), int32(1))
	require.Zero(t, runtime.RuntimeMetricSnapshot(ctx).Gauges["ownership_ready"])
}
