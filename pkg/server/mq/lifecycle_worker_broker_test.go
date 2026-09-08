// 本文件以真实 Redis/NATS 验证多实例轮次互斥；没有 Broker 时明确跳过。
package mq

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type countedLifecycleWorker struct {
	LifecycleMQProvider
	lifecycleWorkerProvider
	inspects atomic.Int64
}

func (p *countedLifecycleWorker) InspectLifecycle(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
	p.inspects.Add(1)
	return p.LifecycleMQProvider.InspectLifecycle(ctx, policy)
}

// TestLifecycleWorkerThirteenByFortyFour 验证稳定空闲期的扫描次数只随实际 owner 轮次增长。
func TestLifecycleWorkerThirteenByFortyFour(t *testing.T) {
	for _, broker := range []string{"redis", "nats"} {
		t.Run(broker, func(t *testing.T) {
			ctx := context.Background()
			var provider LifecycleMQProvider
			if broker == "redis" {
				addr := os.Getenv("CORE_TEST_REDIS_ADDR")
				if addr == "" {
					t.Skip("NOT RUN: CORE_TEST_REDIS_ADDR")
				}
				p := NewRedisStreamProvider(addr, fmt.Sprintf("core:worker:scale:%d", time.Now().UnixNano()), 0)
				require.NoError(t, p.Connect(ctx))
				t.Cleanup(func() { _ = p.Close() })
				provider = p
			} else {
				p, _ := newNATSReliableProvider(t)
				provider = p
			}
			worker := &countedLifecycleWorker{LifecycleMQProvider: provider, lifecycleWorkerProvider: provider.(lifecycleWorkerProvider)}
			controllers := make([]*lifecycleController, 13)
			for i := range controllers {
				controllers[i] = newLifecycleController()
				defer controllers[i].stop()
			}
			policies := make([]LifecyclePolicy, 44)
			for i := range policies {
				policy := validLifecyclePolicy("required")
				policy.Subject = fmt.Sprintf("fills-%02d", i)
				policy.Reclaim = ReclaimBudget{Interval: 2 * time.Second, BatchSize: 2000, TimeBudget: 100 * time.Millisecond}
				policy.Capacity.HardMessages = 1000
				require.NoError(t, provider.EnsureLifecycle(ctx, policy))
				policies[i] = policy
				for _, c := range controllers {
					c.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: worker}
				}
			}
			before := workerRedisCommandCounts(t, provider)
			for round := 0; round < 3; round++ {
				for _, policy := range policies {
					var wg sync.WaitGroup
					errs := make([]error, len(controllers))
					for i, c := range controllers {
						wg.Add(1)
						go func(i int, c *lifecycleController) { defer wg.Done(); errs[i] = c.runOnce(ctx, policy.Subject) }(i, c)
					}
					wg.Wait()
					for _, err := range errs {
						require.NoError(t, err)
					}
					for _, c := range controllers {
						require.NoError(t, c.allowPublish(policy.Subject))
						require.Zero(t, c.reclaimFailed.Load())
					}
				}
			}
			after := workerRedisCommandCounts(t, provider)
			if before != nil {
				for _, name := range []string{"xinfo|groups", "xinfo|stream", "xpending"} {
					delta := after[name] - before[name]
					require.Greater(t, delta, int64(0), name)
					require.LessOrEqual(t, delta, int64(2*3*44), "command amplification: %s", name)
					t.Logf("redis %s delta=%d", name, delta)
				}
				t.Logf("redis evalsha delta=%d (includes cheap lease attempts by all contenders)", after["evalsha"]-before["evalsha"])
				require.LessOrEqual(t, after["evalsha"]-before["evalsha"], int64(3*3*44), "standby lease checks must not amplify Lua calls")
			}
			require.EqualValues(t, 3*44, worker.inspects.Load(), "non-owner must not multiply full scans by 13")
			t.Logf("controllers=13 subjects=44 rounds=3 budget=100ms full_inspects=%d failures=0", worker.inspects.Load())
		})
	}
}

func workerRedisCommandCounts(t *testing.T, provider LifecycleMQProvider) map[string]int64 {
	t.Helper()
	r, ok := provider.(*RedisStreamProvider)
	if !ok {
		return nil
	}
	info, err := r.client.Info(context.Background(), "commandstats").Result()
	require.NoError(t, err)
	counts := make(map[string]int64)
	for _, line := range strings.Split(info, "\n") {
		name, fields, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || !strings.HasPrefix(name, "cmdstat_") {
			continue
		}
		for _, field := range strings.Split(fields, ",") {
			if strings.HasPrefix(field, "calls=") {
				value, err := strconv.ParseInt(strings.TrimPrefix(field, "calls="), 10, 64)
				require.NoError(t, err)
				counts[strings.TrimPrefix(name, "cmdstat_")] = value
			}
		}
	}
	return counts
}

// TestLifecycleWorkerBrokerOwnership 验证同一主题 13 个竞争者只有一个完整扫描者，续约后仍互斥。
func TestLifecycleWorkerBrokerOwnership(t *testing.T) {
	for _, broker := range []string{"redis", "nats"} {
		t.Run(broker, func(t *testing.T) {
			ctx := context.Background()
			var provider LifecycleMQProvider
			if broker == "redis" {
				addr := os.Getenv("CORE_TEST_REDIS_ADDR")
				if addr == "" {
					t.Skip("NOT RUN: CORE_TEST_REDIS_ADDR")
				}
				p := NewRedisStreamProvider(addr, fmt.Sprintf("core:worker:%d", time.Now().UnixNano()), 0)
				require.NoError(t, p.Connect(ctx))
				t.Cleanup(func() { _ = p.Close() })
				provider = p
			} else {
				p, _ := newNATSReliableProvider(t)
				provider = p
			}
			worker, ok := provider.(lifecycleWorkerProvider)
			require.True(t, ok, "built-in provider must acquire ownership before Inspect")
			policy := validLifecyclePolicy("required")
			policy.Reclaim = ReclaimBudget{Interval: 2 * time.Second, BatchSize: 2000, TimeBudget: 100 * time.Millisecond}
			require.NoError(t, provider.EnsureLifecycle(ctx, policy))
			winner := -1
			for round := 0; round < 3; round++ {
				acquired := make([]bool, 13)
				errs := make([]error, 13)
				var wg sync.WaitGroup
				for i := range acquired {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						_, acquired[i], errs[i] = worker.acquireLifecycleRound(ctx, policy, fmt.Sprintf("owner-%d", i))
					}(i)
				}
				wg.Wait()
				count := 0
				for i := range acquired {
					require.NoError(t, errs[i])
					if acquired[i] {
						count++
						if winner < 0 {
							winner = i
						}
						require.Equal(t, winner, i)
					}
				}
				require.Equal(t, 1, count)
			}
			// 显式停旧 owner 后才可接管，standby 不得提前顶替。
			releaser := provider.(lifecycleWorkerReleaser)
			require.NoError(t, releaser.releaseLifecycleRound(ctx, policy, fmt.Sprintf("owner-%d", winner)))
			_, acquired, err := worker.acquireLifecycleRound(ctx, policy, "replacement")
			require.NoError(t, err)
			require.True(t, acquired)
			require.NoError(t, releaser.releaseLifecycleRound(ctx, policy, fmt.Sprintf("owner-%d", winner)))
			_, acquired, err = worker.acquireLifecycleRound(ctx, policy, "intruder")
			require.NoError(t, err)
			require.False(t, acquired, "old owner release must not remove replacement")
		})
	}
}

// TestRedisLifecycleWorkerNetworkDeadline 验证真实 Redis 请求遵守轮次 deadline，而不是默认秒级 socket 超时。
func TestRedisLifecycleWorkerNetworkDeadline(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" || os.Getenv("CORE_TEST_LIFECYCLE_PAUSE_REDIS") != "1" {
		t.Skip("NOT RUN: requires dedicated Redis and CORE_TEST_LIFECYCLE_PAUSE_REDIS=1")
	}
	ctx := context.Background()
	p := NewRedisStreamProvider(addr, fmt.Sprintf("core:worker:deadline:%d", time.Now().UnixNano()), 0)
	require.NoError(t, p.Connect(ctx))
	defer p.Close()
	policy := validLifecyclePolicy("required")
	policy.Reclaim.TimeBudget = 100 * time.Millisecond
	require.NoError(t, p.EnsureLifecycle(ctx, policy))
	c := newLifecycleController()
	defer c.stop()
	c.entries[policy.Subject] = &lifecycleEntry{policy: policy, provider: p}
	require.NoError(t, p.client.Do(ctx, "CLIENT", "PAUSE", 200, "ALL").Err())
	start := time.Now()
	err := c.runOnce(ctx, policy.Subject)
	require.Error(t, err)
	require.Equal(t, 0, lifecycleFailureReason(err))
	require.Less(t, time.Since(start), 150*time.Millisecond)
}

// TestRedisLifecycleDirectReclaimDeadline 验证公开直接回收也遵守 policy 预算，不依赖调用方先设置 deadline。
func TestRedisLifecycleDirectReclaimDeadline(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" || os.Getenv("CORE_TEST_LIFECYCLE_PAUSE_REDIS") != "1" {
		t.Skip("NOT RUN: requires dedicated Redis and CORE_TEST_LIFECYCLE_PAUSE_REDIS=1")
	}
	ctx := context.Background()
	p := NewRedisStreamProvider(addr, fmt.Sprintf("core:worker:direct:%d", time.Now().UnixNano()), 0)
	require.NoError(t, p.Connect(ctx))
	defer p.Close()
	policy := validLifecyclePolicy()
	policy.NoRequiredGroups = true
	policy.Retention.MinAge = time.Millisecond
	policy.Reclaim.TimeBudget = 100 * time.Millisecond
	require.NoError(t, p.EnsureLifecycle(ctx, policy))
	snapshot, err := p.InspectLifecycle(ctx, policy)
	require.NoError(t, err)
	require.NoError(t, p.client.Do(ctx, "CLIENT", "PAUSE", 200, "ALL").Err())
	start := time.Now()
	_, err = p.ReclaimLifecycle(ctx, policy, snapshot)
	require.Error(t, err)
	require.Less(t, time.Since(start), 150*time.Millisecond)
}
