// 本文件用隔离 Redis 验证消费去重及成功后 ACK，错误 schema 必须保留在 PEL。
package reference

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestNativeConsumerValidatesBeforeACK 验证重复事件不重复记录，非法事件不能提前确认。
func TestNativeConsumerValidatesBeforeACK(t *testing.T) {
	addr := os.Getenv("CORE_REFERENCE_TEST_REDIS")
	if addr == "" {
		t.Skip("需要显式独占 Redis")
	}
	ctx := context.Background()
	r := redis.NewClient(&redis.Options{Addr: addr})
	defer r.Close()
	e := r.XGroupCreateMkStream(ctx, streamName, "shop-user", "0").Err()
	if !isBusyGroup(e) {
		require.NoError(t, e)
	}
	p := orderChanged{SchemaVersion: 1, EventID: "native-ack-test-" + time.Now().String(), EventType: "OrderCreated", OrderID: 3, UserID: 720001}
	for _, valid := range []bool{true, true, false} {
		if !valid {
			p.SchemaVersion = 0
		}
		b, e := json.Marshal(p)
		require.NoError(t, e)
		data, e := json.Marshal(envelope{Type: "OrderCreated", Data: b})
		require.NoError(t, e)
		require.NoError(t, r.XAdd(ctx, &redis.XAddArgs{Stream: streamName, Values: map[string]interface{}{"data": data}}).Err())
	}
	entries, e := r.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "shop-user", Consumer: "native-ack-test", Streams: []string{streamName, ">"}, Count: 32, Block: time.Second}).Result()
	require.NoError(t, e)
	a := &application{ctx: ctx, redis: r}
	for _, entry := range entries {
		a.handleMessages(entry.Messages)
	}
	count := 0
	a.processed.Range(func(_, _ interface{}) bool { count++; return true })
	require.Equal(t, 1, count)
	pending, e := r.XPending(ctx, streamName, "shop-user").Result()
	require.NoError(t, e)
	require.EqualValues(t, 1, pending.Count)
}
