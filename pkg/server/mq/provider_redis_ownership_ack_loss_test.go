// 本文件使用真实Redis执行ACK后向Provider注入响应丢失，验证重查PEL而非异常重放。
package mq

import (
	"context"
	"errors"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type ownedAckLossHook struct{ lost atomic.Bool }

func (*ownedAckLossHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) { return next(ctx, network, addr) }
}
func (*ownedAckLossHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *ownedAckLossHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		args := cmd.Args()
		if err == nil && len(args) > 1 && ((cmd.Name() == "eval" && strings.Contains(fmt.Sprint(args[1]), "acked=acked+redis.call(\"XACK\"")) || (cmd.Name() == "evalsha" && fmt.Sprint(args[1]) == redis.NewScript(redisOwnedBatchACK).Hash())) && !h.lost.Swap(true) {
			return errors.New("受控ACK已经在Broker执行，但调用方未取得响应")
		}
		return err
	}
}

func TestRedisOwnedAckResponseLostDoesNotReplayConfirmedWork(t *testing.T) {
	p, _, m := ownershipFixture(t)
	ctx := context.Background()
	require.NoError(t, p.BindOwnership(ctx, m))
	hook := &ownedAckLossHook{}
	p.client.AddHook(hook)
	var calls atomic.Int64
	runtime, err := p.SubscribeOwnedBatch(ctx, OwnedReliableOptions{Manifest: m, InstanceID: "ack", SessionID: "boot", MaxOwned: 1, NoDueTasks: true, Key: func(*Message) (string, error) { return "A", nil }, Activate: func(context.Context, ownership.Credential) error { return nil }}, func(context.Context, ownership.Credential, []*Message) error { calls.Add(1); return nil })
	require.NoError(t, err)
	defer runtime.Close(ctx)
	require.NoError(t, p.Publish(ctx, m.Subjects[0], []byte("fixture"), &PublishOptions{OrderingKey: "A"}))
	require.Eventually(t, func() bool { return hook.lost.Load() }, 3*time.Second, 10*time.Millisecond)
	require.Zero(t, p.client.XPending(ctx, p.streamKey(m.Subjects[0]), m.Group).Val().Count)
	time.Sleep(350 * time.Millisecond)
	require.EqualValues(t, 1, calls.Load())
	require.Positive(t, p.deliveryMetrics.ackFailed.Load())
	require.Zero(t, p.deliveryMetrics.ackSucceeded.Load(), "未知响应不能计为确认成功")
}
