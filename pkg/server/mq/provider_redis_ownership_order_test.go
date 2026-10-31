// 本文件验证原OrderingKey可以跨scope key；批次不得越过其最早未完成前序。
package mq

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"strings"
	"sync"
	"testing"
)

func TestRedisOwnershipPreservesOriginalKeyAcrossScopeKeys(t *testing.T) {
	p, s, m := ownershipFixture(t)
	ctx := context.Background()
	require.NoError(t, p.BindOwnership(ctx, m))
	subject := m.Subjects[0]
	for _, data := range []string{"A:1", "B:2", "A:3"} {
		require.NoError(t, p.Publish(ctx, subject, []byte(data), &PublishOptions{OrderingKey: "same-user"}))
	}
	_, err := p.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: m.Group, Consumer: "prefetch", Streams: []string{p.streamKey(subject), ">"}, Count: 8, Block: -1}).Result()
	require.NoError(t, err)
	var received []string
	readers := map[string]*redisOwnedReader{}
	credentials := map[string]ownership.Credential{}
	for _, key := range m.Keys {
		c, err := s.Acquire(ctx, ownershipCandidate(m, key, key), m.Timing.Lease)
		require.NoError(t, err)
		require.NoError(t, s.Ready(ctx, c))
		credentials[key] = c
		readers[key] = newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, InstanceID: key, SessionID: key, NoDueTasks: true, Key: func(msg *Message) (string, error) { return strings.Split(string(msg.Data), ":")[0], nil }}, func(_ context.Context, _ ownership.Credential, rows []*Message) error {
			for _, row := range rows {
				received = append(received, string(row.Data))
			}
			return nil
		}, &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics})
	}
	progress, err := readers["B"].step(ctx, credentials["B"])
	require.NoError(t, err)
	require.False(t, progress, "B不能越过A的前序")
	progress, err = readers["A"].step(ctx, credentials["A"])
	require.NoError(t, err)
	require.True(t, progress)
	require.Equal(t, []string{"A:1"}, received, "批次不能把A:3越过B:2")
	progress, err = readers["B"].step(ctx, credentials["B"])
	require.NoError(t, err)
	require.True(t, progress)
	progress, err = readers["A"].step(ctx, credentials["A"])
	require.NoError(t, err)
	require.True(t, progress)
	require.Equal(t, []string{"A:1", "B:2", "A:3"}, received)
}

type ownershipReadGapHook struct {
	once   sync.Once
	inject func()
}

func (h *ownershipReadGapHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *ownershipReadGapHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *ownershipReadGapHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		args := cmd.Args()
		if cmd.Name() == "eval" && len(args) > 1 && strings.Contains(args[1].(string), "XREADGROUP") {
			h.once.Do(h.inject)
		}
		return next(ctx, cmd)
	}
}

func TestRedisOwnershipNewReadRescansPendingBeforeAdmission(t *testing.T) {
	p, _, m := ownershipFixture(t)
	ctx := context.Background()
	require.NoError(t, p.BindOwnership(ctx, m))
	subject := m.Subjects[0]
	for _, data := range []string{"A:1", "B:2"} {
		require.NoError(t, p.Publish(ctx, subject, []byte(data), &PublishOptions{OrderingKey: "same-user"}))
	}
	admin := redis.NewClient(&redis.Options{Addr: p.addr, PoolSize: 1})
	defer admin.Close()
	p.client.AddHook(&ownershipReadGapHook{inject: func() {
		_, err := admin.XReadGroup(ctx, &redis.XReadGroupArgs{Group: m.Group, Consumer: "another-instance", Streams: []string{p.streamKey(subject), ">"}, Count: 1, Block: -1}).Result()
		require.NoError(t, err)
	}})
	reader := newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, InstanceID: "b", SessionID: "boot-b", Key: func(msg *Message) (string, error) { return strings.Split(string(msg.Data), ":")[0], nil }}, nil, &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics})
	rows, err := reader.subjectMessages(ctx, subject, true)
	require.NoError(t, err)
	selected, err := reader.selectMessages(subject, "B", rows)
	require.NoError(t, err)
	require.Empty(t, selected, "读取窗口间进入PEL的A必须挡住B")
}
