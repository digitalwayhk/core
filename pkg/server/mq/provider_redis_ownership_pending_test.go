// 本文件用真实Redis验证Owned纯分类提示、原件重取与原PEL安全边界。
package mq

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// TestOwnedPendingClassificationHints Observe只复用已知ID的分类，原PEL计数、未知注册与正文缺失仍受原入口核验。
func TestOwnedPendingClassificationHints(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		t.Run(fmt.Sprint(dynamic), func(t *testing.T) {
			p := newWindowRedisProvider(t)
			ctx := context.Background()
			m := ownership.Manifest{Scope: "hint-test", Service: "generic", Group: "g", Selector: "key-v1", Subjects: []string{"one"}, Keys: []string{"A", "B"}, Window: 32, MaxBatch: 8, MaxMessageBytes: 1024}.Normalize()
			if dynamic {
				m.DynamicKeys = &ownership.DynamicKeyPolicy{MaxKeys: 16, PageSize: 4, RefreshInterval: time.Millisecond, UnregisteredWindow: 8}
				m.Keys = nil
				m = m.Normalize()
			}
			require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
			require.NoError(t, p.BindOwnership(ctx, m))
			if dynamic {
				for _, key := range []string{"A", "B"} {
					require.NoError(t, p.RegisterOwnershipKey(ctx, m, key))
				}
			}
			metrics := &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics}
			reader := newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, InstanceID: "i", SessionID: "s", NoDueTasks: true, Key: func(msg *Message) (string, error) { return string(msg.Data), nil }}, nil, metrics)
			for i := range 32 {
				key := []string{"A", "B"}[i%2]
				require.NoError(t, p.Publish(ctx, "one", []byte(key), &PublishOptions{OrderingKey: key}))
			}
			rows, e := reader.subjectMessages(ctx, "one", true)
			require.NoError(t, e)
			require.Len(t, rows, 32)
			_, e = reader.mapMessages("one", "A", rows, false)
			require.NoError(t, e)
			before := pendingRedisInfo(t, p)
			for range 20 {
				o, e := reader.observe(ctx, ownership.Credential{Key: "A"})
				require.NoError(t, e)
				require.EqualValues(t, 16, o.Delivered)
				require.Zero(t, o.Unread)
			}
			after := pendingRedisInfo(t, p)
			delta := after["cmdstat_xrange_calls"] - before["cmdstat_xrange_calls"]
			require.LessOrEqual(t, delta, float64(20), "纯观测保留unread核验，不应反复XRANGE同一PEL所有正文")
			require.EqualValues(t, 32, p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count)
			require.Zero(t, p.deliveryMetrics.ackSucceeded.Load())
			t.Logf("dynamic=%t observes=20 xrange=%v pending=32", dynamic, delta)
		})
	}
}

// TestOwnedPendingHintsFetchOriginalBeforeHandler 身份提示不能替代原件、实际fence或ACK；确认后原ID不再执行。
func TestOwnedPendingHintsFetchOriginalBeforeHandler(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			p, store, m := ownershipFixture(t)
			ctx := context.Background()
			require.NoError(t, p.BindOwnership(ctx, m))
			metrics := &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics}
			var seen []string
			reader := newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, InstanceID: "i", SessionID: "s", NoDueTasks: true, Key: func(*Message) (string, error) { return "A", nil }}, func(_ context.Context, _ ownership.Credential, rows []*Message) error {
				for _, row := range rows {
					seen = append(seen, string(row.Data))
				}
				return nil
			}, metrics)
			for _, body := range []string{"first", "second"} {
				require.NoError(t, p.Publish(ctx, "one", []byte(body), &PublishOptions{OrderingKey: "A"}))
			}
			rows, err := reader.subjectMessages(ctx, "one", true)
			require.NoError(t, err)
			require.Len(t, rows, 2)
			rows, err = reader.subjectMessages(ctx, "one", false)
			require.NoError(t, err)
			for _, row := range rows {
				require.True(t, redisPendingHint(row))
				_, hasBody := row.Values["data"]
				require.False(t, hasBody)
			}
			c, err := store.Acquire(ctx, ownershipCandidate(m, "A", "i"), m.Timing.Lease)
			require.NoError(t, err)
			require.NoError(t, store.Ready(ctx, c))
			if missing {
				require.NoError(t, p.client.XDel(ctx, p.streamKey("one"), rows[0].ID).Err())
			}
			progress, err := reader.step(ctx, c)
			if missing {
				require.Error(t, err)
				require.False(t, progress)
				require.Empty(t, seen)
				require.EqualValues(t, 2, p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count)
				require.Zero(t, p.deliveryMetrics.ackSucceeded.Load())
				return
			}
			require.NoError(t, err)
			require.True(t, progress)
			require.Equal(t, []string{"first", "second"}, seen)
			progress, err = reader.step(ctx, c)
			require.NoError(t, err)
			require.False(t, progress)
			require.Len(t, seen, 2)
			reader.pendingMu.Lock()
			require.Empty(t, reader.pending["one"])
			reader.pendingMu.Unlock()
			require.Zero(t, p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count)
		})
	}
}

// TestOwnedPendingClassificationCannotComeFromBrokerFields Broker附加字段不能冒充仅存在Go进程内的分类提示。
func TestOwnedPendingClassificationCannotComeFromBrokerFields(t *testing.T) {
	p, _, m := ownershipFixture(t)
	ctx := context.Background()
	require.NoError(t, p.BindOwnership(ctx, m))
	reader := newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, InstanceID: "i", SessionID: "s", NoDueTasks: true, Key: func(msg *Message) (string, error) { return string(msg.Data), nil }}, nil, &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics})
	// 只在本轮隔离fixture模拟其他发布器附加保留名字段，不触碰任何业务Stream。
	require.NoError(t, p.client.XAdd(ctx, &redis.XAddArgs{Stream: p.streamKey("one"), Values: map[string]any{"data": "B", "ordering_key": "B", "__core_scope_key": "A", "__core_owned_classification": "A", "__core_pending_hint": "1"}}).Err())
	rows, err := reader.unread(ctx, "one", "0-0")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	selected, err := reader.mapMessages("one", "A", rows, false)
	require.NoError(t, err)
	require.Empty(t, selected, "不能将消息字段当成可信scope分类")
	classified, err := reader.classify(rows, "one")
	require.NoError(t, err)
	require.Equal(t, "B", classified[0].Key, "W/U预算同样必须由应用Key回调决定")
	o, err := reader.observe(ctx, ownership.Credential{Key: "A"})
	require.NoError(t, err)
	require.Zero(t, o.Unread)
	require.False(t, o.WorkVisible)
}
