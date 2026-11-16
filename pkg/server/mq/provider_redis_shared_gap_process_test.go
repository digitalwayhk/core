// 本文件以真实Redis和三个独立消费者测量同key后继间隙；不把受控handler或组件结果当业务容量。
package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type sharedGapEvent struct {
	Kind     string   `json:"kind"`
	ID       string   `json:"id,omitempty"`
	IDs      []string `json:"ids,omitempty"`
	Start    int64    `json:"start"`
	End      int64    `json:"end"`
	Result   int64    `json:"result,omitempty"`
	Key      int      `json:"key,omitempty"`
	Sequence int      `json:"sequence,omitempty"`
	PID      int      `json:"pid"`
}

type sharedGapRecorder struct {
	mu     sync.Mutex
	file   *os.File
	tokens map[string]string
}

func (h *sharedGapRecorder) record(e sharedGapEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e.PID = os.Getpid()
	_ = json.NewEncoder(h.file).Encode(e)
}

// DialHook透传真实网络，不改变连接预算。
func (*sharedGapRecorder) DialHook(n redis.DialHook) redis.DialHook { return n }

// ProcessPipelineHook只记录合成测试的批次时钟，不记录正文或token。
func (h *sharedGapRecorder) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now().UnixNano()
		err := next(ctx, cmds)
		result := int64(len(cmds))
		if err != nil {
			result = -1
		}
		h.record(sharedGapEvent{Kind: "body_pipeline", Start: start, End: time.Now().UnixNano(), Result: result})
		return err
	}
}

// ProcessHook记录稳定阶段与合成消息ID，不复制命令参数或凭据。
func (h *sharedGapRecorder) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		args := cmd.Args()
		kind := cmd.Name()
		id := ""
		if len(args) > 1 && (kind == "eval" || kind == "evalsha") {
			script := fmt.Sprint(args[1])
			switch script {
			case redisSharedAcquireScript.Hash():
				kind = "acquire"
				id = fmt.Sprint(args[7])
			case redisSharedReleaseScript.Hash():
				kind = "release"
				h.mu.Lock()
				id = h.tokens[fmt.Sprint(args[4])]
				h.mu.Unlock()
			case redisReliablePlainAckScript.Hash():
				kind = "ack"
				id = fmt.Sprint(args[6])
			case redisSharedReadScript:
				kind = "new_read"
			case redisSharedPendingHintScript:
				kind = "pel_read"
			default:
				if strings.Contains(script, "SHARED_PENDING_BODY_MISSING") && len(args) == 5 {
					kind = "body"
					id = fmt.Sprint(args[4])
				} else {
					kind = "control"
				}
			}
		}
		start := time.Now().UnixNano()
		err := next(ctx, cmd)
		end := time.Now().UnixNano()
		var result int64
		if kind == "acquire" && err == nil {
			result = cmd.(*redis.Cmd).Val().(int64)
			if result == 1 {
				h.mu.Lock()
				h.tokens[fmt.Sprint(args[8])] = id
				h.mu.Unlock()
			}
		}
		if kind == "release" {
			h.mu.Lock()
			delete(h.tokens, fmt.Sprint(args[4]))
			h.mu.Unlock()
		}
		if err != nil {
			result = -1
		}
		var ids []string
		if err == nil {
			if c, ok := cmd.(*redis.XStreamSliceCmd); ok {
				for _, stream := range c.Val() {
					for _, m := range stream.Messages {
						ids = append(ids, m.ID)
					}
				}
			}
			if c, ok := cmd.(*redis.XAutoClaimCmd); ok {
				rows, _, e := c.Result()
				if e == nil {
					for _, row := range rows {
						ids = append(ids, row.ID)
					}
				}
			}
		}
		h.record(sharedGapEvent{Kind: kind, ID: id, IDs: ids, Start: start, End: end, Result: result})
		return err
	}
}

// TestRedisSharedKeySuccessorGapProcesses只在显式诊断环境运行；父进程保留原始事件与各独立PID。
func TestRedisSharedKeySuccessorGapProcesses(t *testing.T) {
	root := os.Getenv("CORE_SHARED_GAP_ROOT")
	if root == "" {
		t.Skip("NOT_RUN: CORE_SHARED_GAP_ROOT and isolated CORE_TEST_REDIS_ADDR")
	}
	if worker := os.Getenv("CORE_SHARED_GAP_WORKER"); worker != "" {
		f, err := os.Create(filepath.Join(root, "events-"+worker+".jsonl"))
		require.NoError(t, err)
		defer f.Close()
		rec := &sharedGapRecorder{file: f, tokens: map[string]string{}}
		p := NewRedisStreamProviderWithConfig(config.RedisStreamMQConfig{Addr: os.Getenv("CORE_TEST_REDIS_ADDR"), Prefix: os.Getenv("CORE_SHARED_GAP_PREFIX"), CommandPoolSize: 32, BlockingReadPoolSize: 16})
		ctx, cancel := context.WithCancel(context.WithValue(context.Background(), "core_gap_diagnostic", func(stage, id string) {
			now := time.Now().UnixNano()
			rec.record(sharedGapEvent{Kind: stage, ID: id, Start: now, End: now})
		}))
		defer cancel()
		require.NoError(t, p.Connect(ctx))
		p.client.AddHook(rec)
		stop, err := p.SubscribeReliable(ctx, "events", ReliableSubscribeOptions{Group: "workers", Consumer: worker, SharedKeys: true, KeyConcurrency: 32, Count: 128}, func(m *Message) error {
			var key, seq int
			if _, e := fmt.Sscanf(string(m.Data), "%d:%d", &key, &seq); e != nil {
				return e
			}
			start := time.Now().UnixNano()
			if os.Getenv("CORE_SHARED_GAP_CRASH") == "1" && key == 0 && seq == 0 {
				marker, _ := json.Marshal(map[string]any{"id": m.ID, "pid": os.Getpid()})
				if e := os.WriteFile(filepath.Join(root, "crash.ready"), marker, 0600); e != nil {
					return e
				}
				<-ctx.Done()
				return ctx.Err()
			}
			time.Sleep(90 * time.Millisecond)
			rec.record(sharedGapEvent{Kind: "handler", ID: m.ID, Start: start, End: time.Now().UnixNano(), Key: key, Sequence: seq})
			return nil
		})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(root, "ready-"+worker), []byte(strconv.Itoa(os.Getpid())), 0600))
		for {
			if _, err := os.Stat(filepath.Join(root, "stop")); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		stop()
		require.NoError(t, p.Close())
		return
	}
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	require.NoError(t, os.MkdirAll(root, 0700))
	var children []*exec.Cmd
	var logs []*os.File
	defer func() {
		_ = os.WriteFile(filepath.Join(root, "stop"), nil, 0600)
		for _, c := range children {
			if c.ProcessState == nil {
				_ = c.Process.Kill()
				_ = c.Wait()
			}
		}
		for _, f := range logs {
			_ = f.Close()
		}
	}()
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("p%d", i)
		log, err := os.Create(filepath.Join(root, name+".log"))
		require.NoError(t, err)
		logs = append(logs, log)
		c := exec.Command(os.Args[0], "-test.run=^TestRedisSharedKeySuccessorGapProcesses$", "-test.v", "-test.timeout=60s")
		c.Env = append(os.Environ(), "GOMAXPROCS=2", "CORE_SHARED_GAP_WORKER="+name, "CORE_SHARED_GAP_PREFIX="+p.prefix)
		if workerAddr := os.Getenv("CORE_SHARED_GAP_WORKER_ADDR"); workerAddr != "" {
			c.Env = append(c.Env, "CORE_TEST_REDIS_ADDR="+workerAddr)
		}
		c.Stdout = log
		c.Stderr = log
		require.NoError(t, c.Start())
		children = append(children, c)
	}
	require.Eventually(t, func() bool {
		for i := 0; i < 3; i++ {
			if _, err := os.Stat(filepath.Join(root, fmt.Sprintf("ready-p%d", i))); err != nil {
				return false
			}
		}
		return true
	}, 10*time.Second, 10*time.Millisecond)
	ticks := 50
	if v := os.Getenv("CORE_SHARED_GAP_TICKS"); v != "" {
		var e error
		ticks, e = strconv.Atoi(v)
		require.NoError(t, e)
	}
	start := time.Now()
	published := 0
	for n := 0; n < ticks; n++ {
		deadline := start.Add(time.Duration(n) * 100 * time.Millisecond)
		if d := time.Until(deadline); d > 0 {
			time.Sleep(d)
		}
		for k := 0; k < 50; k++ {
			require.NoError(t, p.Publish(ctx, "events", []byte(fmt.Sprintf("%d:%d", k, n)), &PublishOptions{OrderingKey: fmt.Sprintf("key-%d", k)}))
			published++
		}
	}
	require.Eventually(t, func() bool {
		pending, err := p.client.XPending(ctx, p.streamKey("events"), "workers").Result()
		if err != nil || pending.Count != 0 {
			return false
		}
		groups, err := p.client.XInfoGroups(ctx, p.streamKey("events")).Result()
		return err == nil && len(groups) == 1 && groups[0].Lag == 0
	}, 30*time.Second, 10*time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(root, "stop"), nil, 0600))
	for _, c := range children {
		require.NoError(t, c.Wait())
	}
	for _, f := range logs {
		_ = f.Close()
	}
	byKey := map[int][]sharedGapEvent{}
	countByPID := map[int]int{}
	var gaps []float64
	var same []float64
	all := 0
	for i := 0; i < 3; i++ {
		b, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("events-p%d.jsonl", i)))
		require.NoError(t, err)
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			var e sharedGapEvent
			require.NoError(t, json.Unmarshal([]byte(line), &e))
			if e.Kind == "handler" {
				byKey[e.Key] = append(byKey[e.Key], e)
				countByPID[e.PID]++
				all++
			}
		}
	}
	require.Equal(t, published, all)
	require.GreaterOrEqual(t, len(countByPID), 2, "必须多个独立进程实际处理，不能只启动进程")
	for _, events := range byKey {
		sort.Slice(events, func(i, j int) bool { return events[i].Start < events[j].Start })
		require.Len(t, events, ticks)
		for i, e := range events {
			require.Equal(t, i, e.Sequence)
			if i > 0 {
				d := float64(e.Start-events[i-1].End) / 1e6
				require.GreaterOrEqual(t, d, float64(0))
				gaps = append(gaps, d)
				if e.PID == events[i-1].PID {
					same = append(same, d)
				}
			}
		}
	}
	stat := func(x []float64) map[string]float64 {
		sort.Float64s(x)
		if len(x) == 0 {
			return nil
		}
		avg := float64(0)
		for _, v := range x {
			avg += v
		}
		return map[string]float64{"count": float64(len(x)), "mean": avg / float64(len(x)), "p50": x[len(x)/2], "p95": x[(len(x)-1)*95/100], "p99": x[(len(x)-1)*99/100], "max": x[len(x)-1]}
	}
	summary := map[string]any{"published": published, "handlers": all, "per_process": countByPID, "handler_end_to_next_start_ms": stat(gaps), "same_process_gap_ms": stat(same), "wall_seconds": time.Since(start).Seconds(), "ticks": ticks, "handler_ms": 90, "concurrency": 32, "prefetch": 128, "keys": 50, "processes": 3}
	b, err := json.MarshalIndent(summary, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "summary.json"), b, 0600))
	t.Log(string(b))
}

// 保持接口形状由编译器验证，测试hook不改变原命令。
var _ redis.Hook = (*sharedGapRecorder)(nil)
