package mq_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type redisLifecycleHarness struct {
	t        *testing.T
	ctx      context.Context
	prefix   string
	subject  string
	stream   string
	provider *mq.RedisStreamProvider
	client   *redis.Client
}

func newRedisLifecycleHarness(t *testing.T) *redisLifecycleHarness {
	t.Helper()
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT RUN: 设置 CORE_TEST_REDIS_ADDR 后运行 Redis lifecycle 真 Broker 测试")
	}
	prefix := fmt.Sprintf("core:test:lifecycle:%d", time.Now().UnixNano())
	provider := mq.NewRedisStreamProvider(addr, prefix, 0)
	require.NoError(t, provider.Connect(context.Background()))
	client := redis.NewClient(&redis.Options{Addr: addr})
	h := &redisLifecycleHarness{
		t: t, ctx: context.Background(), prefix: prefix, subject: "fills",
		stream: prefix + ":fills", provider: provider, client: client,
	}
	t.Cleanup(func() {
		keys, _ := client.Keys(context.Background(), prefix+"*").Result()
		if len(keys) > 0 {
			_ = client.Del(context.Background(), keys...).Err()
		}
		_ = client.Close()
		_ = provider.Close()
	})
	return h
}

func (h *redisLifecycleHarness) policy(mode mq.LifecycleMode, groups ...mq.ConsumerGroupRequirement) mq.LifecyclePolicy {
	return mq.LifecyclePolicy{
		Subject: h.subject, Mode: mode, RequiredGroups: groups,
		Retention: mq.RetentionPolicy{MinAge: 0},
		Reclaim:   mq.ReclaimBudget{Interval: time.Hour, BatchSize: 2, TimeBudget: time.Second},
	}
}

func group(name string, start mq.ConsumerStartPosition) mq.ConsumerGroupRequirement {
	return mq.ConsumerGroupRequirement{Name: name, Start: start}
}

func (h *redisLifecycleHarness) publish(body string) string {
	h.t.Helper()
	require.NoError(h.t, h.provider.Publish(h.ctx, h.subject, []byte(body), nil))
	items, err := h.client.XRevRangeN(h.ctx, h.stream, "+", "-", 1).Result()
	require.NoError(h.t, err)
	require.Len(h.t, items, 1)
	return items[0].ID
}

func (h *redisLifecycleHarness) read(groupName string, count int64) []redis.XMessage {
	h.t.Helper()
	result, err := h.client.XReadGroup(h.ctx, &redis.XReadGroupArgs{
		Group: groupName, Consumer: "test-consumer", Streams: []string{h.stream, ">"}, Count: count,
	}).Result()
	require.NoError(h.t, err)
	require.Len(h.t, result, 1)
	return result[0].Messages
}

func (h *redisLifecycleHarness) ack(groupName string, messages []redis.XMessage) {
	h.t.Helper()
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	require.NoError(h.t, h.client.XAck(h.ctx, h.stream, groupName, ids...).Err())
}

// TestRedisLifecycleOfflineRequiredGroupBlocksReclaim 验证离线必需组不会被当作已完成。
func TestRedisLifecycleOfflineRequiredGroupBlocksReclaim(t *testing.T) {
	h := newRedisLifecycleHarness(t)
	policy := h.policy(mq.LifecycleModeEnforce, group("online", mq.StartFromAllRetained), group("offline", mq.StartFromAllRetained))
	require.NoError(t, h.provider.EnsureLifecycle(h.ctx, policy))
	h.publish("m1")
	online := h.read("online", 1)
	h.ack("online", online)

	snapshot, err := h.provider.InspectLifecycle(h.ctx, policy)
	require.NoError(t, err)
	result, err := h.provider.ReclaimLifecycle(h.ctx, policy, snapshot)
	require.NoError(t, err)
	require.Zero(t, result.Reclaimed)
	require.Equal(t, int64(1), h.client.XLen(h.ctx, h.stream).Val())
}

// TestRedisLifecyclePendingMessageBlocksReclaim 验证漏 ACK 消息保留在 PEL 与 Stream 中。
func TestRedisLifecyclePendingMessageBlocksReclaim(t *testing.T) {
	h := newRedisLifecycleHarness(t)
	policy := h.policy(mq.LifecycleModeEnforce, group("positions", mq.StartFromAllRetained))
	require.NoError(t, h.provider.EnsureLifecycle(h.ctx, policy))
	h.publish("m1")
	h.read("positions", 1)

	snapshot, err := h.provider.InspectLifecycle(h.ctx, policy)
	require.NoError(t, err)
	result, err := h.provider.ReclaimLifecycle(h.ctx, policy, snapshot)
	require.NoError(t, err)
	require.Zero(t, result.Reclaimed)
	require.Equal(t, int64(1), h.client.XPending(h.ctx, h.stream, "positions").Val().Count)
	require.Equal(t, int64(1), h.client.XLen(h.ctx, h.stream).Val())
}

// TestRedisLifecycleAllGroupsPermitBoundedReclaim 验证全部组 ACK 后单轮仍不超过 BatchSize。
func TestRedisLifecycleAllGroupsPermitBoundedReclaim(t *testing.T) {
	h := newRedisLifecycleHarness(t)
	policy := h.policy(mq.LifecycleModeEnforce, group("users", mq.StartFromAllRetained), group("positions", mq.StartFromAllRetained))
	require.NoError(t, h.provider.EnsureLifecycle(h.ctx, policy))
	for _, body := range []string{"m1", "m2", "m3", "m4"} {
		h.publish(body)
	}
	for _, groupName := range []string{"users", "positions"} {
		messages := h.read(groupName, 4)
		h.ack(groupName, messages)
	}

	snapshot, err := h.provider.InspectLifecycle(h.ctx, policy)
	require.NoError(t, err)
	result, err := h.provider.ReclaimLifecycle(h.ctx, policy, snapshot)
	require.NoError(t, err)
	require.Equal(t, int64(2), result.Reclaimed)
	require.Equal(t, int64(2), h.client.XLen(h.ctx, h.stream).Val())
}

// TestRedisLifecycleStartFromNewSkipsExistingHistory 验证 new-only 组不会因预建而误消费历史。
func TestRedisLifecycleStartFromNewSkipsExistingHistory(t *testing.T) {
	h := newRedisLifecycleHarness(t)
	h.publish("history")
	policy := h.policy(mq.LifecycleModeObserve, group("new-reader", mq.StartFromNew))
	require.NoError(t, h.provider.EnsureLifecycle(h.ctx, policy))
	newID := h.publish("new")

	messages := h.read("new-reader", 10)
	require.Len(t, messages, 1)
	require.Equal(t, newID, messages[0].ID)
}

// TestRedisLifecycleMissingRequiredGroupFailsClosed 验证运行期人工删组后 Core 停止回收。
func TestRedisLifecycleMissingRequiredGroupFailsClosed(t *testing.T) {
	h := newRedisLifecycleHarness(t)
	policy := h.policy(mq.LifecycleModeEnforce, group("positions", mq.StartFromAllRetained))
	require.NoError(t, h.provider.EnsureLifecycle(h.ctx, policy))
	require.NoError(t, h.client.XGroupDestroy(h.ctx, h.stream, "positions").Err())

	_, err := h.provider.InspectLifecycle(h.ctx, policy)
	require.ErrorIs(t, err, mq.ErrLifecycleStateUncertain)
}
