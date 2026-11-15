// 本文件显式运行同机可靠配置持续输入对照；功能通过不等于消费方容量通过。
package nosql

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
)

// TestIndependentGroupsPerformance 显式比较相同target并发预算下旧整轮和独立组，保留每轮Save及真实确认分布。
func TestIndependentGroupsPerformance(t *testing.T) {
	if os.Getenv("CORE_TEST_GROUP_PERFORMANCE") != "1" {
		t.Skip("持续输入性能对照须显式启用")
	}
	logx.SetLevel(logx.ErrorLevel)
	defer logx.SetLevel(logx.InfoLevel)
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for round := 1; round <= 3; round++ {
		for _, workers := range []int{0, 4} {
			t.Run(fmt.Sprintf("round=%d/workers=%d", round, workers), func(t *testing.T) {
				target := &cleanupTestTarget{}
				target.sync = func(ctx context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
					var wg sync.WaitGroup
					for _, item := range items {
						wg.Add(1)
						go func(group string) {
							defer wg.Done()
							delay := time.Millisecond
							if group == "group-00" {
								delay = 40 * time.Millisecond
							}
							timer := time.NewTimer(delay)
							defer timer.Stop()
							select {
							case <-timer.C:
							case <-ctx.Done():
							}
						}(item.Item.Group)
					}
					wg.Wait()
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					result := &WriteBehindResult{}
					for _, item := range items {
						result.ConfirmedKeys = append(result.ConfirmedKeys, item.Key)
					}
					return result, nil
				}
				path := t.TempDir()
				cfg := DefaultProductionConfig(path)
				cfg.EnableLogger = false
				cfg.AutoSync = true
				cfg.SyncBatchSize = 4
				cfg.SyncFlushThreshold = 4
				cfg.SyncMaxCollectDelay = time.Millisecond
				cfg.WriteBehindFailure = failureTestPolicy()
				cfg.WriteBehindFailure.ScanLimit = 16
				store, _, e := NewReliableWriteStore[cleanupTestRecord](ServiceIdentity{ServiceName: "group-perf", DataCenterID: 1, MachineID: 1}, ReliableWriteStoreConfig{BasePath: path, Badger: cfg, Admission: WriteAdmissionConfig{MaxConcurrent: 128, AcquireTimeout: 2 * time.Second}, Batch: BatchCommitConfig{MaxBatch: 128, CollectWindow: time.Millisecond, CollectBacklog: 1, QueueCapacity: 1024}})
				require.NoError(t, e)
				require.NoError(t, store.UseWriteBehindWithFailureHandlerOptions(target, target, WriteBehindExecutionOptions{IndependentGroups: workers}))
				defer func() { _ = store.Close(context.Background()); _ = CloseSharedManager(store.config.BasePath) }()
				var mu sync.Mutex
				var saves, healthy, slow []float64
				var failures int
				var firstError error
				var peakPending int
				var input sync.WaitGroup
				var started sync.Map
				store.db.disposition.confirmationObserver = func(items []*SyncQueueItem[cleanupTestRecord]) {
					for _, item := range items {
						value, ok := started.Load(item.Key)
						if !ok {
							panic("缺少真实确认的开始时间")
						}
						elapsed := float64(time.Since(value.(time.Time))) / float64(time.Millisecond)
						mu.Lock()
						if item.Item.Group == "group-00" {
							slow = append(slow, elapsed)
						} else {
							healthy = append(healthy, elapsed)
						}
						mu.Unlock()
					}
				}
				var cpuBefore, cpuAfter syscall.Rusage
				_ = syscall.Getrusage(syscall.RUSAGE_SELF, &cpuBefore)
				var memBefore, memAfter runtime.MemStats
				runtime.ReadMemStats(&memBefore)
				start := time.Now()
				for group := 0; group < 16; group++ {
					input.Add(1)
					go func(group int) {
						defer input.Done()
						for seq := 0; seq < 120; seq++ {
							scheduled := start.Add(time.Duration(seq) * 50 * time.Millisecond)
							if wait := time.Until(scheduled); wait > 0 {
								time.Sleep(wait)
							}
							key := fmt.Sprintf("%02d-%04d", group, seq)
							record := &cleanupTestRecord{Key: key, Group: fmt.Sprintf("group-%02d", group), Revision: 1, ProbeID: strings.Repeat("x", 2028) + key}
							record.NewModel()
							saveStart := time.Now()
							started.Store(store.db.prefix+key, saveStart)
							err := store.Save(context.Background(), record)
							elapsed := float64(time.Since(saveStart)) / float64(time.Millisecond)
							pending := store.Metrics().Pending
							mu.Lock()
							saves = append(saves, elapsed)
							if err != nil {
								failures++
								if firstError == nil {
									firstError = err
								}
							}
							if pending > peakPending {
								peakPending = pending
							}
							mu.Unlock()
						}
					}(group)
				}
				input.Wait()
				inputEnd := time.Now()
				pendingAtEnd := store.Metrics().Pending
				mu.Lock()
				t.Logf("真实测量计数 saves=%d errors=%d confirmation_samples=%d first_error=%v", len(saves), failures, len(healthy)+len(slow), firstError)
				mu.Unlock()
				require.Eventually(t, func() bool { return store.Metrics().Pending == 0 }, 30*time.Second, 5*time.Millisecond)
				drained := time.Now() // 以真实pending已为0的观测时点计排空，不后移到结果读取。
				require.Eventually(t, func() bool {
					mu.Lock()
					count := len(healthy) + len(slow)
					mu.Unlock()
					return count == 1920 && store.Metrics().Sync.SyncedItems == 1920
				}, time.Second, time.Millisecond)
				metricsReady := time.Now()
				_ = syscall.Getrusage(syscall.RUSAGE_SELF, &cpuAfter)
				runtime.ReadMemStats(&memAfter)
				m := store.Metrics()
				mu.Lock()
				data := map[string]any{"round": round, "independent_groups": workers, "gomaxprocs": 1, "sync_writes": true, "detect_conflicts": true, "items": len(saves), "errors": failures, "input_seconds": inputEnd.Sub(start).Seconds(), "drain_seconds": drained.Sub(inputEnd).Seconds(), "wall_seconds": drained.Sub(start).Seconds(), "post_drain_metrics_wait_seconds": metricsReady.Sub(drained).Seconds(), "save_ms": groupPercentiles(saves), "healthy_save_begin_to_local_confirmation_ms": groupPercentiles(healthy), "slow_save_begin_to_local_confirmation_ms": groupPercentiles(slow), "confirmed": m.Sync.SyncedItems, "confirm_per_wall_second": float64(m.Sync.SyncedItems) / drained.Sub(start).Seconds(), "pending_at_input_end": pendingAtEnd, "peak_pending": peakPending, "pending_final": m.Pending, "batch": m.Batch, "sync_metrics": m.Sync, "cpu_user_seconds": float64(cpuAfter.Utime.Nano()-cpuBefore.Utime.Nano()) / 1e9, "cpu_system_seconds": float64(cpuAfter.Stime.Nano()-cpuBefore.Stime.Nano()) / 1e9, "max_rss_bytes": cpuAfter.Maxrss, "heap_alloc_bytes": memAfter.HeapAlloc, "gc_delta": memAfter.NumGC - memBefore.NumGC, "cpu_throttling": nil, "physical_io": nil, "filesystem": "native temporary directory on local APFS; not tmpfs", "target": "controlled timers; no SQL"}
				b, e := json.Marshal(data)
				require.NoError(t, e)
				t.Log(string(b))
				mu.Unlock()
				require.Equal(t, 1920, len(saves))
				require.Zero(t, failures)
				require.EqualValues(t, 1920, m.Sync.SyncedItems)
			})
		}
	}
}
func groupPercentiles(values []float64) map[string]float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	var sum float64
	for _, v := range sorted {
		sum += v
	}
	at := func(q float64) float64 { return sorted[int(float64(len(sorted)-1)*q)] }
	return map[string]float64{"mean": sum / float64(len(sorted)), "p50": at(.5), "p95": at(.95), "p99": at(.99), "max": sorted[len(sorted)-1]}
}
