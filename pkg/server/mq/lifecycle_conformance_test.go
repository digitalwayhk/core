package mq

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRedisMessageLifecycleConformance(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT RUN: 设置 CORE_TEST_REDIS_ADDR 后运行 Redis lifecycle conformance")
	}
	prefix := fmt.Sprintf("core:test:conformance:%d", time.Now().UnixNano())
	provider := NewRedisStreamProvider(addr, prefix, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	require.NoError(t, provider.Connect(ctx))
	defer provider.Close()

	require.NoError(t, VerifyMessageLifecycleConformance(ctx, provider, "fills"))
}

func TestNATSMessageLifecycleConformance(t *testing.T) {
	provider, parent := newNATSReliableProvider(t)
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()

	require.NoError(t, VerifyMessageLifecycleConformance(ctx, provider, "conformance.fills"))
}

// TestRedisNoRequiredGroupsContinuousReclaim 验证真实 Redis 持续发布与回收时保留量不随总发布量增长。
func TestRedisNoRequiredGroupsContinuousReclaim(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT RUN: 缺少 CORE_TEST_REDIS_ADDR")
	}
	provider := NewRedisStreamProvider(addr, fmt.Sprintf("core:nogroups:continuous:%d", time.Now().UnixNano()), 0)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	require.NoError(t, provider.Connect(ctx))
	defer provider.Close()
	verifyNoGroupsContinuousReclaim(t, ctx, provider)
}

// TestNATSNoRequiredGroupsContinuousReclaim 验证真实 JetStream 持续发布与回收时的相同契约。
func TestNATSNoRequiredGroupsContinuousReclaim(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	verifyNoGroupsContinuousReclaim(t, ctx, provider)
}

func verifyNoGroupsContinuousReclaim(t *testing.T, parent context.Context, provider LifecycleMQProvider) {
	t.Helper()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	policy := LifecyclePolicy{Subject: "continuous-history", NoRequiredGroups: true, Mode: LifecycleModeEnforce,
		Retention: RetentionPolicy{MinAge: 100 * time.Millisecond},
		Reclaim:   ReclaimBudget{Interval: time.Hour, BatchSize: 4, TimeBudget: time.Second}}.Normalize()
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	done := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	defer func() { cancel(); wg.Wait() }()
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; i < 40; i++ {
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-ticker.C:
			}
			if err := provider.Publish(ctx, policy.Subject, []byte("history"), nil); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	finished := false
	var reclaimed int64
	for {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case err := <-done:
			require.NoError(t, err)
			finished = true
		case <-ticker.C:
		}
		snapshot, err := provider.InspectLifecycle(ctx, policy)
		require.NoError(t, err)
		result, err := provider.ReclaimLifecycle(ctx, policy, snapshot)
		require.NoError(t, err)
		require.LessOrEqual(t, result.Reclaimed, int64(4))
		reclaimed += result.Reclaimed
		if finished {
			// 发布结束前已持续回收，大多数历史并未堆积到结束后才删除。
			require.Greater(t, reclaimed, int64(20))
			if reclaimed == 40 {
				return
			}
		}
	}
}
