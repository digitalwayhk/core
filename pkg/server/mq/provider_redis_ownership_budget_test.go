// 本文件在持续积压下测量真实Provider的批次/worker/PEL上限及取消后不提前ACK。
package mq

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRedisOwnershipContinuousBacklogBudgetsAndCancel(t *testing.T) {
	p, _, m := ownershipFixture(t)
	ctx := context.Background()
	m.Scope = "budget"
	m.MaxBatch = 3
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	require.NoError(t, p.BindOwnership(ctx, m))
	for _, subject := range m.Subjects {
		for i := 0; i < 64; i++ {
			key := []string{"A", "B"}[i%2]
			require.NoError(t, p.Publish(ctx, subject, []byte(fmt.Sprintf("%s:%d", key, i)), &PublishOptions{OrderingKey: key}))
		}
	}
	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	defer release()
	var active, peak, calls atomic.Int64
	runtime, err := p.SubscribeOwnedBatch(ctx, OwnedReliableOptions{Manifest: m, InstanceID: "budget", SessionID: "boot", MaxOwned: 2, NoDueTasks: true, Key: func(message *Message) (string, error) { return strings.Split(string(message.Data), ":")[0], nil }, Activate: func(context.Context, ownership.Credential) error { return nil }}, func(_ context.Context, _ ownership.Credential, rows []*Message) error {
		require.LessOrEqual(t, len(rows), 3)
		calls.Add(1)
		n := active.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		defer active.Add(-1)
		<-hold
		return nil
	})
	require.NoError(t, err)
	t.Cleanup(func() { release(); _ = runtime.Close(ctx) })
	require.Eventually(t, func() bool { return active.Load() == 2 }, 3*time.Second, 5*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	require.EqualValues(t, 2, calls.Load())
	require.EqualValues(t, 2, peak.Load())
	var pending int64
	for _, subject := range m.Subjects {
		n := p.client.XPending(ctx, p.streamKey(subject), m.Group).Val().Count
		require.LessOrEqual(t, n, m.Window)
		pending += n
	}
	require.Positive(t, pending)
	require.LessOrEqual(t, pending, int64(16))
	require.Zero(t, p.deliveryMetrics.ackSucceeded.Load())
	wait, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, runtime.Close(wait), context.DeadlineExceeded, "取消不能遗弃仍执行的真实handler")
	require.EqualValues(t, 2, active.Load())
	release()
	require.NoError(t, runtime.Close(ctx))
	require.Zero(t, active.Load())
	require.Zero(t, p.deliveryMetrics.ackSucceeded.Load(), "取消后的成功返回不允许ACK")
	var retainedPending int64
	for _, subject := range m.Subjects {
		retainedPending += p.client.XPending(ctx, p.streamKey(subject), m.Group).Val().Count
		require.EqualValues(t, 64, p.client.XLen(ctx, p.streamKey(subject)).Val())
	}
	require.Equal(t, pending, retainedPending, "取消后原PEL不得被确认或删除")
}
