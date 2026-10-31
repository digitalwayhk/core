package mq

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestLifecycleCapacityCountsRetainedHistoryNotOnlyPending(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	m := NewManager()
	m.Register(p)
	require.NoError(t, m.SetCurrent(p.Name()))
	defer m.Close()
	policy := LifecyclePolicy{Subject: "history", Mode: LifecycleModeEnforce, RequiredGroups: []ConsumerGroupRequirement{{Name: "users", Start: StartFromAllRetained}, {Name: "audit", Start: StartFromAllRetained}}, Retention: RetentionPolicy{MinAge: 250 * time.Millisecond}, Capacity: CapacityPolicy{HardMessages: 2}, Reclaim: ReclaimBudget{Interval: 100 * time.Millisecond, BatchSize: 10, TimeBudget: 50 * time.Millisecond}}
	require.NoError(t, m.RequireMessageLifecycle(ctx, policy))
	for i := 0; i < 2; i++ {
		require.NoError(t, m.Publish(ctx, "history", []byte("x"), &PublishOptions{OrderingKey: fmt.Sprint(i)}))
	}
	for _, g := range []string{"users", "audit"} {
		entries, err := p.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: g, Consumer: g, Streams: []string{p.streamKey("history"), ">"}, Count: 2}).Result()
		require.NoError(t, err)
		if g == "users" {
			for _, item := range entries[0].Messages {
				require.NoError(t, p.client.XAck(ctx, p.streamKey("history"), g, item.ID).Err())
			}
		}
	}
	require.Eventually(t, func() bool {
		_ = m.lifecycle.runOnce(ctx, "history")
		return m.lifecycle.allowPublish("history") == ErrLifecycleBackpressure
	}, time.Second, 10*time.Millisecond)
	require.ErrorIs(t, m.Publish(ctx, "history", []byte("denied"), nil), ErrLifecycleBackpressure)
	require.Zero(t, p.client.XPending(ctx, p.streamKey("history"), "users").Val().Count)
	require.EqualValues(t, 2, p.client.XPending(ctx, p.streamKey("history"), "audit").Val().Count)
	// 等原保留期自然结束，audit 的未完成消息仍必须保留，不改 policy。
	started := time.Now()
	require.Eventually(t, func() bool {
		_ = m.lifecycle.runOnce(ctx, "history")
		return time.Since(started) > 300*time.Millisecond
	}, time.Second, 10*time.Millisecond)
	require.EqualValues(t, 2, p.client.XLen(ctx, p.streamKey("history")).Val())
	pending, err := p.client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: p.streamKey("history"), Group: "audit", Start: "-", End: "+", Count: 2}).Result()
	require.NoError(t, err)
	for _, item := range pending {
		require.NoError(t, p.client.XAck(ctx, p.streamKey("history"), "audit", item.ID).Err())
	}
	require.Eventually(t, func() bool {
		_ = m.lifecycle.runOnce(ctx, "history")
		return p.client.XLen(ctx, p.streamKey("history")).Val() == 0
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		_ = m.lifecycle.runOnce(ctx, "history")
		return m.Publish(ctx, "history", []byte("resumed"), nil) == nil
	}, time.Second, 10*time.Millisecond)
}
