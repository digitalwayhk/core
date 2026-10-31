// 本文件验证原组/PEL保全迁移及真实多主题业务批次，不以逐条回调冒充批量。
package mq

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRedisOwnershipBatchPreservesPendingAndOldVersionRefuses(t *testing.T) {
	p, s, m := ownershipFixture(t)
	ctx := context.Background()
	for _, subject := range m.Subjects {
		require.NoError(t, p.client.XGroupCreateMkStream(ctx, p.streamKey(subject), m.Group, "0").Err())
		require.NoError(t, p.ensureReliableGroupMode(ctx, subject, ReliableSubscribeOptions{Group: m.Group, SharedKeys: true, Count: 8}))
		for i := 0; i < 4; i++ {
			require.NoError(t, p.Publish(ctx, subject, []byte(fmt.Sprintf("A:%d", i)), &PublishOptions{OrderingKey: "A"}))
		}
		_, err := p.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: m.Group, Consumer: "old-consumer", Streams: []string{p.streamKey(subject), ">"}, Count: 1, Block: -1}).Result()
		require.NoError(t, err)
	}
	require.NoError(t, p.BindOwnership(ctx, m))
	_, err := p.SubscribeReliable(ctx, m.Subjects[0], ReliableSubscribeOptions{Group: m.Group, SharedKeys: true, Count: 8}, func(*Message) error { return nil })
	require.ErrorIs(t, err, ErrReliableGroupModeConflict)
	batches := make(chan []*Message, 8)
	r, err := p.SubscribeOwnedBatch(ctx, OwnedReliableOptions{Manifest: m, InstanceID: "one", SessionID: "boot-one", MaxOwned: 1, NoDueTasks: true, Key: func(msg *Message) (string, error) { return strings.Split(string(msg.Data), ":")[0], nil }, Activate: func(context.Context, ownership.Credential) error { return nil }}, func(_ context.Context, c ownership.Credential, messages []*Message) error {
		require.Equal(t, "A", c.Key)
		batches <- messages
		return nil
	})
	require.NoError(t, err)
	defer r.Close(ctx)
	received := 0
	multi := false
	for received < 8 {
		select {
		case rows := <-batches:
			received += len(rows)
			multi = multi || len(rows) > 1
			require.LessOrEqual(t, len(rows), m.MaxBatch)
		case <-time.After(3 * time.Second):
			t.Fatal("原pending和新消息没有形成业务批次")
		}
	}
	require.True(t, multi, "至少一个批次必须含多条原输入")
	for _, subject := range m.Subjects {
		require.Eventually(t, func() bool { return p.client.XPending(ctx, p.streamKey(subject), m.Group).Val().Count == 0 }, time.Second, time.Millisecond)
		require.EqualValues(t, 4, p.client.XLen(ctx, p.streamKey(subject)).Val())
	}
	_, err = s.Resolve(ctx, m.Scope, "A")
	require.NoError(t, err)
	require.ErrorIs(t, p.UnbindOwnership(ctx, m), ownership.ErrBusy)
	require.NoError(t, r.Close(ctx))
	require.NoError(t, p.UnbindOwnership(ctx, m))
	stop, err := p.SubscribeReliable(ctx, m.Subjects[0], ReliableSubscribeOptions{Group: m.Group, SharedKeys: true, Count: 8}, func(*Message) error { return nil })
	require.NoError(t, err)
	stop()
}

func TestRedisOwnershipBindingRefusesLiveOldSharedExecution(t *testing.T) {
	p, _, m := ownershipFixture(t)
	ctx := context.Background()
	entered, hold := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	stop, err := p.SubscribeReliable(ctx, m.Subjects[0], ReliableSubscribeOptions{Group: m.Group, SharedKeys: true, KeyConcurrency: 2, Count: 8}, func(*Message) error { close(entered); <-hold; return nil })
	require.NoError(t, err)
	t.Cleanup(func() { release(); stop() })
	require.NoError(t, p.Publish(ctx, m.Subjects[0], []byte("A:one"), &PublishOptions{OrderingKey: "A"}))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("旧SharedKeys真实执行未开始")
	}
	require.ErrorIs(t, p.BindOwnership(ctx, m), ownership.ErrBusy, "不能只换mode标记就让旧代码仍可有效ACK")
	release()
	require.Eventually(t, func() bool { return p.client.XPending(ctx, p.streamKey(m.Subjects[0]), m.Group).Val().Count == 0 }, time.Second, time.Millisecond)
	stop()
	p.wg.Wait()
	require.NoError(t, p.BindOwnership(ctx, m))
}
