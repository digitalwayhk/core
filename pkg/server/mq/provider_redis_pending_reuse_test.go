// 本文件用真实Provider的在途消息屏障核对PEL正文重复读取，不提前ACK或伪造进展。
package mq

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	stdjson "encoding/json"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type pendingBodyReadHook struct {
	reads   atomic.Int64
	missing atomic.Int64
	mu      sync.Mutex
	byID    map[string]int
}

// DialHook 透传真实连接建立，不模拟Broker。
func (h *pendingBodyReadHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) { return next(ctx, network, addr) }
}

// ProcessPipelineHook 透传真实管线，不改消息。
func (h *pendingBodyReadHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// ProcessHook 只计实际返回的PEL正文和轮询数，不记录正文或业务key。
func (h *pendingBodyReadHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err != nil && strings.Contains(err.Error(), "SHARED_PENDING_BODY_MISSING") {
			h.missing.Add(1)
		}
		if c, ok := cmd.(*redis.XAutoClaimCmd); ok && err == nil {
			messages, _, e := c.Result()
			if e == nil {
				h.reads.Add(1)
				h.mu.Lock()
				for _, m := range messages {
					h.byID[m.ID]++
				}
				h.mu.Unlock()
			}
		}
		return err
	}
}

// TestSharedPendingHeldBodiesAreNotRefetched 保持两个key在途、四个尾项在PEL，已知正文不应每轮再传回客户端。
func TestSharedPendingHeldBodiesAreNotRefetched(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	hook := &pendingBodyReadHook{byID: map[string]int{}}
	p.client.AddHook(hook)
	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	defer release()
	var entered atomic.Int64
	stop, err := p.SubscribeReliable(ctx, "held", ReliableSubscribeOptions{Group: "g", Consumer: "one", SharedKeys: true, KeyConcurrency: 2, Count: 8}, func(*Message) error { entered.Add(1); <-hold; return nil })
	require.NoError(t, err)
	defer stop()
	for i := 0; i < 6; i++ {
		require.NoError(t, p.Publish(ctx, "held", []byte(fmt.Sprintf("body-%d", i)), &PublishOptions{OrderingKey: fmt.Sprint(i % 2)}))
	}
	require.Eventually(t, func() bool { return entered.Load() == 2 && hook.reads.Load() >= 10 }, 3*time.Second, time.Millisecond)
	hook.mu.Lock()
	duplicate := 0
	for _, n := range hook.byID {
		if n > 1 {
			duplicate += n - 1
		}
	}
	hook.mu.Unlock()
	require.EqualValues(t, 6, p.client.XPending(ctx, p.streamKey("held"), "g").Val().Count)
	t.Logf("pending=6 handler_inflight=2 poll_calls=%d repeated_body_reads=%d", hook.reads.Load(), duplicate)
	require.Zero(t, duplicate, "SHARED_PENDING_BODY_REFETCH: 已在有限task窗口中等待同key前序的正文被重复读取")
	release()
	require.Eventually(t, func() bool { return p.client.XPending(ctx, p.streamKey("held"), "g").Val().Count == 0 }, 3*time.Second, time.Millisecond)
}

// pendingReadBaseline 是v1.12.0原脚本；测量前核对公开交接的SHA，避免把不同工作当旧版。
const pendingReadBaseline = `
if redis.call("GET",KEYS[2])~=ARGV[4] then return redis.error_reply("SHARED_GROUP_MODE_LOST") end
local start="-"
if ARGV[2]~="0-0" then start="("..ARGV[2] end
local pending=redis.call("XPENDING",KEYS[1],ARGV[1],start,"+",ARGV[3])
local messages={}
local next="0-0"
for _,entry in ipairs(pending) do
  local rows=redis.call("XRANGE",KEYS[1],entry[1],entry[1],"COUNT",1)
  if #rows==0 then return redis.error_reply("SHARED_PENDING_BODY_MISSING") end
  table.insert(messages,rows[1])
end
if #pending==tonumber(ARGV[3]) then next=pending[#pending][1] end
return {next,messages,{}}
`

func pendingRedisInfo(t *testing.T, p *RedisStreamProvider) map[string]float64 {
	t.Helper()
	result := map[string]float64{}
	for _, section := range []string{"stats", "cpu", "commandstats"} {
		text, err := p.client.Info(context.Background(), section).Result()
		require.NoError(t, err)
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			if strings.HasPrefix(key, "cmdstat_") {
				value = strings.TrimPrefix(strings.Split(value, ",")[0], "calls=")
				key += "_calls"
			}
			if n, err := strconv.ParseFloat(value, 64); err == nil {
				result[key] = n
			}
		}
	}
	return result
}

// TestSharedPendingFixedPollMeasurement 是显式开启的只读PEL微基准，不冒充业务吞吐、HTTP或跨进程验收。
func TestSharedPendingFixedPollMeasurement(t *testing.T) {
	if os.Getenv("CORE_TEST_SHARED_POLL_MEASURE") != "1" {
		t.Skip("NOT_RUN: 显式PEL读取微基准")
	}
	sha := sha1.Sum([]byte(pendingReadBaseline))
	require.Equal(t, "b25e2afd1e7b36bba1016faa583069bbf4c49a75", hex.EncodeToString(sha[:]))
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	opts := ReliableSubscribeOptions{Group: "g", Consumer: "seed", SharedKeys: true, Count: 32, KeyConcurrency: 2}
	require.NoError(t, p.ensureReliableGroupMode(ctx, "measure", opts))
	key := p.streamKey("measure")
	require.NoError(t, p.client.XGroupCreateMkStream(ctx, key, "g", "0").Err())
	for i := 0; i < 32; i++ {
		require.NoError(t, p.Publish(ctx, "measure", []byte(strings.Repeat("x", 4096)), &PublishOptions{OrderingKey: fmt.Sprint(i % 8)}))
	}
	streams, err := p.readReliableKeyed(ctx, key, "measure", opts, 32)
	require.NoError(t, err)
	var ids []string
	for _, m := range streams[0].Messages {
		ids = append(ids, m.ID)
	}
	require.Len(t, ids, 32)
	for round := 1; round <= 3; round++ {
		for _, variant := range []string{"old_eval", "skip_eval", "skip_evalsha"} {
			args := []interface{}{opts.Group, "0-0", int64(32), redisSharedMarker(opts)}
			script := pendingReadBaseline
			if variant != "old_eval" {
				script = redisSharedPendingScript
				args = append(args, len(ids))
				for _, id := range ids {
					args = append(args, id)
				}
			}
			if variant == "skip_evalsha" {
				require.NoError(t, redis.NewScript(script).Load(ctx, p.client).Err())
			}
			before := pendingRedisInfo(t, p)
			started := time.Now()
			durations := []float64{}
			bodyReads := 0
			for i := 0; i < 200; i++ {
				callStart := time.Now()
				verb, code := "eval", script
				if variant == "skip_evalsha" {
					verb = "evalsha"
					code = redis.NewScript(script).Hash()
				}
				command := []interface{}{verb, code, 2, key, p.ownerLockKey("measure", "g")}
				command = append(command, args...)
				cmd := redis.NewXAutoClaimCmd(ctx, command...)
				require.NoError(t, p.client.Process(ctx, cmd))
				messages, next, err := cmd.Result()
				require.NoError(t, err)
				require.Equal(t, ids[31], next)
				bodyReads += len(messages)
				durations = append(durations, float64(time.Since(callStart))/float64(time.Millisecond))
			}
			wall := time.Since(started)
			after := pendingRedisInfo(t, p)
			sort.Float64s(durations)
			delta := func(key string) any {
				a, ok := after[key]
				b, bok := before[key]
				if !ok || !bok {
					return nil
				}
				return a - b
			}
			var cpu any
			if _, ok := after["used_cpu_user_main_thread"]; ok {
				cpu = (after["used_cpu_user_main_thread"] - before["used_cpu_user_main_thread"]) + (after["used_cpu_sys_main_thread"] - before["used_cpu_sys_main_thread"])
			}
			row := map[string]any{"round": round, "variant": variant, "calls": 200, "body_rows": bodyReads, "pending": p.client.XPending(ctx, key, "g").Val().Count, "wall_ms": float64(wall) / float64(time.Millisecond), "p50_ms": durations[99], "p95_ms": durations[189], "p99_ms": durations[197], "redis_main_cpu_seconds": cpu, "redis_process_cpu_seconds": (after["used_cpu_user"] - before["used_cpu_user"]) + (after["used_cpu_sys"] - before["used_cpu_sys"]), "redis_input_bytes": delta("total_net_input_bytes"), "redis_output_bytes": delta("total_net_output_bytes"), "xrange_calls": delta("cmdstat_xrange_calls"), "eval_calls": delta("cmdstat_eval_calls"), "evalsha_calls": delta("cmdstat_evalsha_calls")}
			encoded, err := stdjson.Marshal(row)
			require.NoError(t, err)
			t.Logf("PENDING_POLL_MEASURE %s", encoded)
			require.EqualValues(t, 32, p.client.XPending(ctx, key, "g").Val().Count)
		}
	}
}

// TestSharedPendingSkipPreservesCursorAndMissingBody 省正文不改XPENDING页推进；清除内存提示后仍拒绝原件缺失。
func TestSharedPendingSkipPreservesCursorAndMissingBody(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	o := ReliableSubscribeOptions{Group: "g", Consumer: "one", Count: 4, KeyConcurrency: 1, SharedKeys: true}
	key := p.streamKey("skip")
	require.NoError(t, p.ensureReliableGroupMode(ctx, "skip", o))
	require.NoError(t, p.client.XGroupCreateMkStream(ctx, key, "g", "0").Err())
	for i := 0; i < 4; i++ {
		require.NoError(t, p.Publish(ctx, "skip", []byte("original"), &PublishOptions{OrderingKey: "A"}))
	}
	streams, err := p.readReliableKeyed(ctx, key, "skip", o, 4)
	require.NoError(t, err)
	first := streams[0].Messages[0].ID
	page, next, err := p.claimReliableKeyedSkipping(ctx, key, "skip", o, "0-0", 1, []string{first})
	require.NoError(t, err)
	require.Empty(t, page)
	require.Equal(t, first, next)
	page, _, err = p.claimReliableKeyedSkipping(ctx, key, "skip", o, next, 1, nil)
	require.NoError(t, err)
	require.Equal(t, streams[0].Messages[1].ID, page[0].ID)
	require.NoError(t, p.client.XDel(ctx, key, first).Err()) // 仅隔离合成故障，不删除消费方或正式消息。
	_, _, err = p.claimReliableKeyed(ctx, key, "skip", o, "0-0", 4)
	require.ErrorContains(t, err, "SHARED_PENDING_BODY_MISSING")
	require.EqualValues(t, 4, p.client.XPending(ctx, key, "g").Val().Count)
}

// TestSharedPendingScriptCacheFlush 保持原EVAL自定义解码，并用真实NOSCRIPT检验取租约及ACK的成熟客户端回退。
func TestSharedPendingScriptCacheFlush(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	o := ReliableSubscribeOptions{Group: "g", Consumer: "one", Count: 4, KeyConcurrency: 1, SharedKeys: true}
	key := p.streamKey("scripts")
	require.NoError(t, p.ensureReliableGroupMode(ctx, "scripts", o))
	require.NoError(t, p.client.XGroupCreateMkStream(ctx, key, "g", "0").Err())
	require.NoError(t, p.Publish(ctx, "scripts", []byte("original"), &PublishOptions{OrderingKey: "A"}))
	require.NoError(t, p.client.ScriptFlush(ctx).Err())
	rows, err := p.readReliableKeyed(ctx, key, "scripts", o, 4)
	require.NoError(t, err)
	require.Len(t, rows[0].Messages, 1)
	require.NoError(t, p.client.ScriptFlush(ctx).Err())
	pending, _, err := p.claimReliableKeyed(ctx, key, "scripts", o, "0-0", 4)
	require.NoError(t, err)
	require.Equal(t, rows[0].Messages[0].ID, pending[0].ID)
	require.NoError(t, p.client.ScriptFlush(ctx).Err())
	called := 0
	require.NoError(t, p.processSharedReliableMessage(ctx, key, "scripts", o, pending[0], func(*Message) error { called++; return nil }))
	require.Equal(t, 1, called)
	require.Zero(t, p.client.XPending(ctx, key, "g").Val().Count)
}

// TestSharedPendingKnownTailMissingAfterBarrier 验证已知尾项重新取正文时发现缺失，同key后项不能越过或被ACK。
func TestSharedPendingKnownTailMissingAfterBarrier(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	key := p.streamKey("lost-tail")
	hook := &pendingBodyReadHook{byID: map[string]int{}}
	p.client.AddHook(hook)
	for i := 0; i < 3; i++ {
		require.NoError(t, p.Publish(ctx, "lost-tail", []byte(fmt.Sprint(i)), &PublishOptions{OrderingKey: "A"}))
	}
	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	defer release()
	var calls atomic.Int64
	stop, err := p.SubscribeReliable(ctx, "lost-tail", ReliableSubscribeOptions{Group: "g", Consumer: "one", SharedKeys: true, Count: 4, KeyConcurrency: 1}, func(*Message) error { calls.Add(1); <-hold; return nil })
	require.NoError(t, err)
	defer stop()
	require.Eventually(t, func() bool { hook.mu.Lock(); defer hook.mu.Unlock(); return calls.Load() == 1 && len(hook.byID) == 3 }, 3*time.Second, time.Millisecond)
	rows, err := p.client.XRange(ctx, key, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.NoError(t, p.client.XDel(ctx, key, rows[1].ID).Err()) // 只在隔离fixture注入原件损坏。
	release()
	require.Eventually(t, func() bool { return hook.missing.Load() > 0 }, 3*time.Second, time.Millisecond)
	require.EqualValues(t, 1, calls.Load())
	pending, err := p.client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: key, Group: "g", Start: "-", End: "+", Count: 4}).Result()
	require.NoError(t, err)
	require.Len(t, pending, 2)
	require.Equal(t, rows[1].ID, pending[0].ID)
	require.Equal(t, rows[2].ID, pending[1].ID)
}
