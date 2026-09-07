package cluster

// 本文件用真实 Redis 验证服务发现通知有界及通知丢失后的周期对账，不涉及业务 MQ。
import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newRedisEventsProvider(t *testing.T) *RedisProvider {
	t.Helper()
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("设置 CORE_TEST_REDIS_ADDR 后运行真实 Redis 通知流测试")
	}
	p, err := NewRedisProvider(addr, 0, fmt.Sprintf("core:test:discovery-events:%d", time.Now().UnixNano()), time.Minute)
	require.NoError(t, err)
	t.Cleanup(func() {
		// 只清理本测试随机前缀下的键，不清空共享 DB。
		ctx := context.Background()
		var cursor uint64
		for {
			keys, next, err := p.client.Scan(ctx, cursor, p.prefix+":*", 100).Result()
			require.NoError(t, err)
			if len(keys) > 0 {
				require.NoError(t, p.client.Del(ctx, keys...).Err())
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		require.NoError(t, p.Close())
	})
	return p
}

// TestRedisProvider_DiscoveryEventsBounded 验证各写入口独立裁剪，且注册状态和租约仍然有效。
func TestRedisProvider_DiscoveryEventsBounded(t *testing.T) {
	const limit = 10000
	for _, operation := range []string{"Register", "Heartbeat", "Deregister"} {
		t.Run(operation, func(t *testing.T) {
			p := newRedisEventsProvider(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			node := &NodeInfo{ID: "orders-1", ServiceName: "orders", Address: "orders", MachineID: 1}
			require.NoError(t, p.Register(ctx, node))
			first, err := p.client.XRangeN(ctx, p.eventsKey(), "-", "+", 1).Result()
			require.NoError(t, err)
			require.Len(t, first, 1)
			if operation == "Deregister" {
				// 构造旧版本无界历史，确保注销脚本本身执行裁剪，不依赖 Register 先裁剪。
				seedDiscoveryEvents(t, p, 2*limit, 0)
				require.NoError(t, p.Deregister(ctx, node.ID))
			} else {
				for i := 0; i < 2*limit+137; i++ {
					if operation == "Register" {
						require.NoError(t, p.Register(ctx, node))
					} else {
						require.NoError(t, p.Heartbeat(ctx, node.ID))
					}
				}
			}
			length, err := p.client.XLen(ctx, p.eventsKey()).Result()
			require.NoError(t, err)
			t.Logf("%s: XLEN=%d，目标=%d", operation, length, limit)
			// 近似裁剪按 Redis 宏节点回收，允许有界余量，不断言精确等于阈值。
			require.LessOrEqual(t, length, int64(limit+1000))
			require.GreaterOrEqual(t, length, int64(limit))
			old, err := p.client.XRange(ctx, p.eventsKey(), first[0].ID, first[0].ID).Result()
			require.NoError(t, err)
			require.Empty(t, old)
			groups, err := p.client.XInfoGroups(ctx, p.eventsKey()).Result()
			require.NoError(t, err)
			require.Empty(t, groups)
			nodes, err := p.List(ctx, "orders", NodeStatusRunning)
			require.NoError(t, err)
			if operation == "Deregister" {
				require.Empty(t, nodes)
				remaining, err := p.client.Exists(ctx, p.slotKey(node), p.indexKey(node.ID), p.nodeKey(node.ServiceName, node.ID)).Result()
				require.NoError(t, err)
				require.Zero(t, remaining)
			} else {
				require.Len(t, nodes, 1)
				for _, key := range []string{p.slotKey(node), p.indexKey(node.ID), p.nodeKey(node.ServiceName, node.ID)} {
					ttl, err := p.client.PTTL(ctx, key).Result()
					require.NoError(t, err)
					require.Greater(t, ttl, time.Duration(0))
				}
				conflict := &NodeInfo{ID: "other-owner", ServiceName: "orders", MachineID: 1}
				require.ErrorIs(t, p.Register(ctx, conflict), ErrSlotConflict)
				require.NoError(t, p.Deregister(ctx, node.ID))
			}
		})
	}
}

func seedDiscoveryEvents(t *testing.T, p *RedisProvider, count int, maxLen int64) {
	t.Helper()
	ctx := context.Background()
	for offset := 0; offset < count; offset += 500 {
		pipe := p.client.Pipeline()
		for i := offset; i < count && i < offset+500; i++ {
			pipe.XAdd(ctx, &redis.XAddArgs{Stream: p.eventsKey(), MaxLen: maxLen, Approx: true,
				Values: map[string]interface{}{"service": "unrelated", "action": "upsert", "node_id": "noise"}})
		}
		_, err := pipe.Exec(ctx)
		require.NoError(t, err)
	}
}

// TestRedisProvider_WatchReconcilesTrimmedEvents 验证 Watch 暂停期间相关通知被裁剪后，仍由周期对账发现注册及注销。
func TestRedisProvider_WatchReconcilesTrimmedEvents(t *testing.T) {
	for _, operation := range []string{"Register", "Deregister"} {
		t.Run(operation, func(t *testing.T) {
			p := newRedisEventsProvider(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			node := &NodeInfo{ID: "orders-1", ServiceName: "orders", Address: "old", MachineID: 1}
			require.NoError(t, p.Register(ctx, node))
			watcher, err := NewRedisProvider(os.Getenv("CORE_TEST_REDIS_ADDR"), 0, p.prefix, 400*time.Millisecond)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, watcher.Close()) })
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			resume := func() { once.Do(func() { close(release) }) }
			defer resume()
			updates := make(chan []*NodeInfo, 4)
			initial := true
			stop, err := watcher.Watch(ctx, "orders", func(nodes []*NodeInfo) {
				if initial {
					initial = false
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
					}
					return
				}
				select {
				case updates <- nodes:
				case <-ctx.Done():
				}
			})
			require.NoError(t, err)
			defer stop()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("Watch 初始快照未就绪")
			}
			if operation == "Register" {
				require.NoError(t, p.Register(ctx, &NodeInfo{ID: "orders-2", ServiceName: "orders", Address: "new", MachineID: 2}))
			} else {
				require.NoError(t, p.Deregister(ctx, node.ID))
			}
			// 只保留其他服务的通知，阻止相关事件唤醒成为此测试的成功来源。
			seedDiscoveryEvents(t, p, 11000, 10000)
			events, err := p.client.XRange(ctx, p.eventsKey(), "-", "+").Result()
			require.NoError(t, err)
			for _, event := range events {
				require.Equal(t, "unrelated", event.Values["service"])
			}
			resume()
			select {
			case nodes := <-updates:
				if operation == "Register" {
					require.Len(t, nodes, 2)
					require.Equal(t, "orders-2", nodes[1].ID)
				} else {
					require.Empty(t, nodes)
				}
			case <-ctx.Done():
				t.Fatal("通知裁剪后周期对账未收敛")
			}
		})
	}
}
