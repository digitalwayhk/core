// 本文件验证旧超预算PEL的单key观测不能伪造零积压。
package mq

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestRedisOwnershipOversizedPELIsUnknownPerKey(t *testing.T) {
	p, s, m := ownershipFixture(t)
	ctx := context.Background()
	subject := m.Subjects[0]
	for _, subject := range m.Subjects {
		require.NoError(t, p.client.XGroupCreateMkStream(ctx, p.streamKey(subject), m.Group, "0").Err())
	}
	for i := 0; i < 10; i++ {
		key := "B"
		if i >= 8 {
			key = "A"
		}
		require.NoError(t, p.Publish(ctx, subject, []byte(key+":value"), &PublishOptions{OrderingKey: key}))
	}
	require.NoError(t, p.BindOwnership(ctx, m))
	// 专属Broker故障注入：绕过正常窗口制造超预算PEL，检查诊断不能伪造0。
	_, err := p.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: m.Group, Consumer: "old", Streams: []string{p.streamKey(subject), ">"}, Count: 10, Block: -1}).Result()
	require.NoError(t, err)
	c, err := s.Acquire(ctx, ownershipCandidate(m, "A", "a"), m.Timing.Lease)
	require.NoError(t, err)
	reader := newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, NoDueTasks: true, Key: func(msg *Message) (string, error) { return strings.Split(string(msg.Data), ":")[0], nil }}, nil, &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics})
	observation, err := reader.observe(ctx, c)
	require.NoError(t, err)
	require.EqualValues(t, -1, observation.Delivered, "有限扫描未覆盖的A不能报0")
	require.EqualValues(t, -1, observation.Queued)
}

func TestRedisOwnershipOversizedMessageRemainsPendingWithinByteBudget(t *testing.T) {
	p := newPoolBudgetProvider(t, 4, 8)
	ctx := context.Background()
	m := ownership.Manifest{Scope: "bytes", Service: "quotes", Group: "quotes", Selector: "key-v1", Keys: []string{"A"}, Subjects: []string{"one"}, MaxMessageBytes: 64}.Normalize()
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	require.NoError(t, p.BindOwnership(ctx, m))
	require.NoError(t, p.Publish(ctx, "one", []byte(strings.Repeat("x", 128)), &PublishOptions{OrderingKey: "A"}))
	reader := newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, InstanceID: "a", SessionID: "boot", NoDueTasks: true, Key: func(*Message) (string, error) { return "A", nil }}, nil, &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics})
	rows, err := reader.subjectMessages(ctx, "one", true)
	require.ErrorIs(t, err, ownership.ErrCapacity)
	require.Empty(t, rows)
	require.EqualValues(t, 1, p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count, "不能ACK或跳过超预算消息")
}

func TestRedisOwnershipUnreadObservationAlsoHonorsByteLimit(t *testing.T) {
	p := newPoolBudgetProvider(t, 4, 8)
	ctx := context.Background()
	m := ownership.Manifest{Scope: "unread-bytes", Service: "quotes", Group: "quotes", Selector: "key-v1", Keys: []string{"A"}, Subjects: []string{"one"}, MaxMessageBytes: 64}.Normalize()
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	require.NoError(t, p.BindOwnership(ctx, m))
	require.NoError(t, p.Publish(ctx, "one", []byte(strings.Repeat("x", 128)), &PublishOptions{OrderingKey: "A"}))
	reader := newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, NoDueTasks: true, Key: func(*Message) (string, error) { return "A", nil }}, nil, &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics})
	_, err := reader.observe(ctx, ownershipCandidate(m, "A", "a"))
	require.ErrorIs(t, err, ownership.ErrCapacity, "观测路径也不能读取无限正文")
	require.Zero(t, p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count, "纯观测不得建立pending或推进游标")
}

// 原OrderingKey前序属于A时，B虽不能执行，也必须作为B的待办而非空闲。
func TestRedisOwnershipObservationCountsBlockedCrossScopeWork(t *testing.T) {
	p, _, m := ownershipFixture(t)
	ctx := context.Background()
	require.NoError(t, p.BindOwnership(ctx, m))
	for _, body := range []string{"A:one", "B:two"} {
		require.NoError(t, p.Publish(ctx, "one", []byte(body), &PublishOptions{OrderingKey: "same-original"}))
	}
	reader := newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, NoDueTasks: true, Key: func(msg *Message) (string, error) { return strings.Split(string(msg.Data), ":")[0], nil }}, nil, &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics})
	candidate := ownershipCandidate(m, "B", "b")
	before, err := reader.observe(ctx, candidate)
	require.NoError(t, err)
	require.EqualValues(t, 1, before.Unread, "未读B不可因顺序屏障伪装成0")
	_, err = reader.subjectMessages(ctx, "one", true)
	require.NoError(t, err)
	after, err := reader.observe(ctx, candidate)
	require.NoError(t, err)
	require.EqualValues(t, 1, after.Delivered)
	require.EqualValues(t, 1, after.Queued)
	require.True(t, after.HasWork())
}
