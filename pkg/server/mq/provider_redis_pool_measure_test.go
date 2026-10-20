package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type poolMeasureClient struct {
	Role   string
	Client *redis.Client
}

// 旧版只有一个client；修复后的唯一夹具装配差异是收集两个实际client。
func poolMeasureClients(p *RedisStreamProvider) []poolMeasureClient {
	return []poolMeasureClient{{"command", p.client}, {"blocking", p.blockingClient}}
}

type poolPhaseMeasure struct {
	Calls   int     `json:"calls"`
	Seconds float64 `json:"seconds"`
}
type poolMeasureHook struct {
	mu           sync.Mutex
	readAt       map[string]time.Time
	phases       map[string]poolPhaseMeasure
	brokerToRead time.Duration
	readSamples  int
}

func (h *poolMeasureHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) { return next(ctx, network, addr) }
}
func (h *poolMeasureHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *poolMeasureHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		end := time.Now()
		phase := ""
		switch cmd.Name() {
		case "xadd":
			phase = "publish"
		case "evalsha":
			if len(cmd.Args()) > 1 {
				switch fmt.Sprint(cmd.Args()[1]) {
				case redisOwnerRenewScript.Hash():
					phase = "renew_owner"
				case redisOwnerCheckScript.Hash():
					phase = "check_owner"
				case redisReliablePlainAckScript.Hash():
					phase = "ack"
				}
			}
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		if phase != "" {
			item := h.phases[phase]
			item.Calls++
			item.Seconds += end.Sub(start).Seconds()
			h.phases[phase] = item
		}
		if result, ok := cmd.(*redis.XStreamSliceCmd); ok && err == nil {
			for _, stream := range result.Val() {
				for _, item := range stream.Messages {
					if strings.Contains(stream.Stream, ":active") {
						h.readAt[item.ID] = end
						ms, _ := strconv.ParseInt(strings.SplitN(item.ID, "-", 2)[0], 10, 64)
						h.brokerToRead += end.Sub(time.UnixMilli(ms))
						h.readSamples++
					}
				}
			}
		}
		return err
	}
}

func TestRedisPoolFixedWorkloadMeasurement(t *testing.T) {
	if os.Getenv("CORE_TEST_POOL_MEASURE") != "1" {
		t.Skip("NOT_RUN: CORE_TEST_POOL_MEASURE=1")
	}
	for _, cpu := range []int{1, 2} {
		for _, concurrency := range []int{1, 2} {
			t.Run(fmt.Sprintf("cpu=%d/key=%d", cpu, concurrency), func(t *testing.T) {
				old := runtime.GOMAXPROCS(cpu)
				defer runtime.GOMAXPROCS(old)
				total := 10 * cpu
				p := newPoolBudgetProvider(t, 2, total-2)
				clients := poolMeasureClients(p)
				hook := &poolMeasureHook{readAt: map[string]time.Time{}, phases: map[string]poolPhaseMeasure{}}
				for _, entry := range clients {
					entry.Client.AddHook(hook)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				// 固定相同的Broker初始积压，不把发布侧被饿死误当成输入不足。
				for i := 0; i <= 200; i++ {
					require.NoError(t, p.Publish(ctx, "active", []byte(strconv.Itoa(i)), &PublishOptions{OrderingKey: fmt.Sprintf("key-%d", i)}))
				}
				for i := 0; i < total+2; i++ {
					_, err := p.SubscribeReliable(ctx, fmt.Sprintf("idle-%d", i), ReliableSubscribeOptions{Group: "group"}, func(*Message) error { return nil })
					require.NoError(t, err)
				}
				ready, permit := make(chan struct{}), make(chan struct{})
				var once sync.Once
				release := func() { once.Do(func() { close(permit) }) }
				defer release()
				var completed atomic.Int64
				var handlerNanos atomic.Int64
				var readToHandler atomic.Int64
				var samples atomic.Int64
				stop, err := p.SubscribeReliable(ctx, "active", ReliableSubscribeOptions{Group: "group", KeyConcurrency: concurrency, Count: 8}, func(m *Message) error {
					if string(m.Data) == "0" {
						close(ready)
						<-permit
						return nil
					}
					start := time.Now()
					hook.mu.Lock()
					read := hook.readAt[m.ID]
					delete(hook.readAt, m.ID)
					hook.mu.Unlock()
					if !read.IsZero() {
						readToHandler.Add(start.Sub(read).Nanoseconds())
						samples.Add(1)
					}
					time.Sleep(2 * time.Millisecond)
					handlerNanos.Add(time.Since(start).Nanoseconds())
					completed.Add(1)
					return nil
				})
				require.NoError(t, err)
				defer stop()
				select {
				case <-ready:
				case <-ctx.Done():
					t.Fatal("测量主题未就绪")
				}
				baseline := map[string]*redis.PoolStats{}
				for _, entry := range clients {
					baseline[entry.Role] = entry.Client.PoolStats()
				}
				hook.mu.Lock()
				hook.brokerToRead = 0
				hook.readSamples = 0
				hook.phases = map[string]poolPhaseMeasure{}
				hook.mu.Unlock()
				started := time.Now()
				release()
				publishSamples := make(chan []time.Duration, 1)
				go func() {
					values := make([]time.Duration, 0, 3)
					for i := 0; i < 3; i++ {
						s := time.Now()
						_ = p.Publish(ctx, "publish-probe", []byte("probe"), nil)
						values = append(values, time.Since(s))
					}
					publishSamples <- values
				}()
				timer := time.NewTimer(3 * time.Second)
				<-timer.C
				elapsed := time.Since(started)
				done := completed.Load()
				stats := map[string]any{}
				for _, entry := range clients {
					s := entry.Client.PoolStats()
					b := baseline[entry.Role]
					stats[entry.Role] = map[string]any{"limit": entry.Client.Options().PoolSize, "max_active": entry.Client.Options().MaxActiveConns, "total": s.TotalConns, "idle": s.IdleConns, "wait_count": s.WaitCount - b.WaitCount, "wait_seconds": float64(s.WaitDurationNs-b.WaitDurationNs) / 1e9, "timeouts": s.Timeouts - b.Timeouts}
				}
				hook.mu.Lock()
				phases := make(map[string]poolPhaseMeasure, len(hook.phases))
				for k, v := range hook.phases {
					phases[k] = v
				}
				brokerAge, readSamples := hook.brokerToRead.Seconds(), hook.readSamples
				hook.mu.Unlock()
				cancel()
				require.NoError(t, p.Close())
				var publishes []time.Duration
				select {
				case publishes = <-publishSamples:
				case <-time.After(time.Second):
					t.Fatal("发布探针未退出")
				}
				report := map[string]any{"gomaxprocs": cpu, "key_concurrency": concurrency, "idle_subscriptions": total + 2, "total_budget": total, "handler_delay_ms": 2, "window_seconds": elapsed.Seconds(), "completed": done, "handled_per_second": float64(done) / elapsed.Seconds(), "pools": stats, "command_phases": phases, "broker_to_read_seconds_sum": brokerAge, "broker_to_read_samples": readSamples, "read_to_handler_seconds_sum": float64(readToHandler.Load()) / 1e9, "read_to_handler_samples": samples.Load(), "handler_seconds_sum": float64(handlerNanos.Load()) / 1e9, "publish_samples_ns": publishes}
				raw, _ := json.Marshal(report)
				t.Log("POOL_MEASUREMENT " + string(raw))
				require.Greater(t, done, int64(0))
			})
		}
	}
}
