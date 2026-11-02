// 本文件在固定预算下记录原版/候选的组件对照，不能认证消费方容量。
package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOwnedDirectoryFixedWorkloadMeasurement(t *testing.T) {
	if os.Getenv("CORE_TEST_OWNED_MEASURE") != "1" {
		t.Skip("NOT_RUN: CORE_TEST_OWNED_MEASURE")
	}
	mode := os.Getenv("CORE_TEST_OWNED_MODE")
	if mode == "" {
		mode = "fixed"
	}
	oldP := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(oldP)
	for round := 0; round < 3; round++ {
		p := newPoolBudgetProvider(t, 4, 8)
		ctx := context.Background()
		m := ownership.Manifest{Scope: fmt.Sprintf("measure-%d", round), Service: "generic", Group: "generic", Selector: "key-v1", Subjects: []string{"one"}, Keys: []string{"A", "B"}, Window: 8, MaxBatch: 8, MaxMessageBytes: 64 << 10, Timing: ownership.Timing{Lease: 3 * time.Second, Renew: 500 * time.Millisecond, Poll: 10 * time.Millisecond, Execution: time.Second, Stall: 5 * time.Second, Observation: 2 * time.Second}}.Normalize()
		if mode == "dynamic" {
			raw, _ := json.Marshal(m)
			var values map[string]any
			require.NoError(t, json.Unmarshal(raw, &values))
			values["Keys"] = []string{}
			values["DynamicKeys"] = map[string]any{"MaxKeys": 16, "PageSize": 2, "RefreshInterval": 10000000, "UnregisteredWindow": 8}
			raw, _ = json.Marshal(values)
			require.NoError(t, json.Unmarshal(raw, &m))
			m = m.Normalize()
		}
		require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
		require.NoError(t, p.BindOwnership(ctx, m))
		if mode == "dynamic" {
			registry, ok := any(p).(interface {
				RegisterOwnershipKey(context.Context, ownership.Manifest, string) error
			})
			require.True(t, ok)
			require.NoError(t, registry.RegisterOwnershipKey(ctx, m, "A"))
			require.NoError(t, registry.RegisterOwnershipKey(ctx, m, "B"))
		}
		var mu sync.Mutex
		latencies := []int64{}
		seen := map[string]bool{}
		handler := func(_ context.Context, _ ownership.Credential, rows []*Message) error {
			mu.Lock()
			defer mu.Unlock()
			for _, row := range rows {
				fields := strings.Split(string(row.Data), ":")
				sent, err := strconv.ParseInt(fields[2], 10, 64)
				if err != nil {
					return err
				}
				if seen[row.ID] {
					return fmt.Errorf("unexpected replay")
				}
				seen[row.ID] = true
				latencies = append(latencies, time.Now().UnixNano()-sent)
			}
			return nil
		}
		sessions := []*ownership.Runtime{}
		for _, id := range []string{"one", "two"} {
			r, err := p.SubscribeOwnedBatch(ctx, OwnedReliableOptions{Manifest: m, InstanceID: id, SessionID: id, MaxOwned: 1, NoDueTasks: true, Key: func(msg *Message) (string, error) { return strings.Split(string(msg.Data), ":")[0], nil }, Activate: func(context.Context, ownership.Credential) error { return nil }}, handler)
			require.NoError(t, err)
			sessions = append(sessions, r)
		}
		store, _ := p.OwnershipStore(ctx)
		require.Eventually(t, func() bool {
			_, a := store.Resolve(ctx, m.Scope, "A")
			_, b := store.Resolve(ctx, m.Scope, "B")
			return a == nil && b == nil
		}, 3*time.Second, time.Millisecond)
		started := time.Now()
		const count = 256
		for i := 0; i < count; i++ {
			key := "A"
			if i%2 == 1 {
				key = "B"
			}
			data := []byte(fmt.Sprintf("%s:%d:%d:padding-0123456789", key, i, time.Now().UnixNano()))
			require.NoError(t, p.Publish(ctx, "one", data, &PublishOptions{OrderingKey: key}))
		}
		publishFinished := time.Now()
		require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(latencies) == count }, 10*time.Second, time.Millisecond)
		require.Eventually(t, func() bool { return p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count == 0 }, 3*time.Second, time.Millisecond)
		wall := time.Since(started)
		mu.Lock()
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		var total int64
		for _, v := range latencies {
			total += v
		}
		quant := func(q float64) float64 {
			return float64(latencies[min(len(latencies)-1, int(float64(len(latencies))*q))]) / 1e6
		}
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		result := map[string]any{"mode": mode, "round": round, "completed": count, "errors": 0, "pending": 0, "gomaxprocs": 2, "window": 8, "bytes_per_message_limit": 64 << 10, "max_owned_per_runtime": 1, "runtimes": 2, "messages_per_second": float64(count) / wall.Seconds(), "publish_to_handler_mean_ms": float64(total) / float64(count) / 1e6, "p50_ms": quant(.5), "p95_ms": quant(.95), "p99_ms": quant(.99), "max_ms": quant(1), "wall_ms": float64(wall) / 1e6, "drain_ms": float64(time.Since(publishFinished)) / 1e6, "heap_alloc_bytes": memory.HeapAlloc, "goroutines": runtime.NumGoroutine(), "cpu_throttle": "NOT_COLLECTED", "io": "NOT_COLLECTED"}
		raw, _ := json.Marshal(result)
		t.Log("MEASURE " + string(raw))
		mu.Unlock()
		for _, r := range sessions {
			require.NoError(t, r.Close(ctx))
		}
	}
}
