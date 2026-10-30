// 本文件提供显式运行的三模式本地可靠写对照，使用Core合成Gob与真实磁盘，不连接消费方。
package nosql

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/stretchr/testify/require"
)

type confirmationLoadRecord struct {
	*entity.Model
	Key     string
	Group   int
	Payload []byte
}

// NewModel 初始化测试模型，不从消费方引入领域模型。
func (r *confirmationLoadRecord) NewModel() {
	if r.Model == nil {
		r.Model = entity.NewModel()
	}
}

// GetLocalKey 返回仅本测试使用的合成原件标识。
func (r *confirmationLoadRecord) GetLocalKey() string { return r.Key }

type confirmationLoadTarget struct{ calls, items, nanos atomic.Int64 }

// SyncBatch 立即确认全部原身份，不进行SQL或网络调用。
func (a *confirmationLoadTarget) SyncBatch(_ context.Context, items []*SyncQueueItem[confirmationLoadRecord]) (*WriteBehindResult, error) {
	start := time.Now()
	r := &WriteBehindResult{}
	for _, item := range items {
		r.ConfirmedKeys = append(r.ConfirmedKeys, item.Key)
	}
	a.calls.Add(1)
	a.items.Add(int64(len(items)))
	a.nanos.Add(time.Since(start).Nanoseconds())
	return r, nil
}

// OrderingGroup 用合成组验证现有分组合同，不引入用户业务语义。
func (a *confirmationLoadTarget) OrderingGroup(item *SyncQueueItem[confirmationLoadRecord]) (string, error) {
	return fmt.Sprint(item.Item.Group), nil
}

// HandleFailure 在性能对照出现意外未知时保留原件并暂停，禁止伪造成功。
func (a *confirmationLoadTarget) HandleFailure(_ context.Context, f WriteBehindFailure[confirmationLoadRecord]) (WriteBehindDecision, error) {
	return WriteBehindDecision{ID: f.Record.ID, Version: f.Record.Snapshot.UpdatedAt, Fingerprint: f.Record.Fingerprint, Action: WriteBehindPause, Scope: WriteBehindScopeKey, Outcome: WriteBehindStillUnknown}, nil
}

func confirmationPayload(t *testing.T) []byte {
	t.Helper()
	// 完整具名Gob，补齐到现场相同的2028字节；只含合成字段，不读取业务模型或订单。
	for n := 1500; n < 2028; n++ {
		var b bytes.Buffer
		err := gob.NewEncoder(&b).Encode(struct {
			OrderID, Market, Side, Price, Quantity, State string
			Revision                                      int
			Detail                                        []byte
		}{"synthetic", "TEST", "BUY", "100.25", "2", "pending", 1, bytes.Repeat([]byte("x"), n)})
		require.NoError(t, err)
		if b.Len() == 2028 {
			return b.Bytes()
		}
	}
	t.Fatal("无法生成2028字节Gob")
	return nil
}

// TestConfirmationPerformanceComparison 显式生成三轮短窗和每模式20秒持续窗口，默认跳过而非冒称性能通过。
func TestConfirmationPerformanceComparison(t *testing.T) {
	if os.Getenv("CORE_CONFIRM_PERF") != "1" {
		t.Skip("显式CORE_CONFIRM_PERF=1运行同机对照")
	}
	root := os.Getenv("CORE_CONFIRM_PERF_OUTPUT")
	require.NotEmpty(t, root)
	require.NoError(t, os.MkdirAll(root, 0700))
	payload := confirmationPayload(t)
	for round := 0; round < 4; round++ {
		for _, mode := range []string{"plain_no_sync", "bounded_no_sync", "bounded_fast_sync"} {
			t.Run(fmt.Sprintf("%d/%s", round, mode), func(t *testing.T) {
				path, err := os.MkdirTemp(root, fmt.Sprintf("%d-%s-", round, mode))
				require.NoError(t, err)
				cfg := DefaultProductionConfig(path)
				cfg.AutoSync = mode == "bounded_fast_sync"
				cfg.EnableLogger = false
				cfg.SyncBatchSize = 128
				cfg.SyncFlushThreshold = 32
				cfg.SyncMaxCollectDelay = 20 * time.Millisecond
				cfg.SyncBacklogDrainDelay = 0
				if mode != "plain_no_sync" {
					cfg.WriteBehindFailure = &WriteBehindFailurePolicy{StoreID: "core-confirm-performance", MaxAttempts: 3, MaxElapsed: 5 * time.Minute, MinRetryInterval: time.Second, MaxRetryInterval: 10 * time.Second, HandlerMaxAttempts: 8, HandlerTimeout: 2 * time.Second, HandlerWorkers: 2, ScanLimit: 128}
				}
				// 参数实验只接受固定白名单，不改变生产默认值和可靠性。
				variant := os.Getenv("CORE_CONFIRM_PERF_VARIANT")
				batch := BatchCommitConfig{MaxBatch: 128, CollectBacklog: 1, CollectWindow: time.Millisecond, QueueCapacity: 1024}
				switch variant {
				case "", "core":
				case "mem64":
					cfg.MemTableSize = 64 << 20
				case "vlog64":
					cfg.ValueLogFileSize = 64 << 20
				case "collect250us":
					batch.CollectWindow = 250 * time.Microsecond
				case "compactors4":
					cfg.NumCompactors = 4
				default:
					t.Fatalf("未知参数实验: %q", variant)
				}
				store, _, err := NewReliableWriteStore[confirmationLoadRecord](ServiceIdentity{ServiceName: "core-confirm-performance"}, ReliableWriteStoreConfig{BasePath: path, Badger: cfg, Batch: batch, Admission: WriteAdmissionConfig{MaxConcurrent: 128, AcquireTimeout: 2 * time.Second, SoftPending: 5000, HardPending: 10000, MaxBacklogDuration: 5 * time.Minute, HardDiskBytes: 1 << 30}, CloseTimeout: time.Second})
				require.NoError(t, err)
				effective := store.db.manager.db.Opts()
				require.True(t, effective.SyncWrites)
				require.True(t, effective.DetectConflicts)
				effectiveOptions := map[string]any{
					"sync_writes": effective.SyncWrites, "detect_conflicts": effective.DetectConflicts,
					"memtable_bytes": effective.MemTableSize, "num_memtables": effective.NumMemtables,
					"compactors": effective.NumCompactors, "l0_tables": effective.NumLevelZeroTables,
					"l0_stall": effective.NumLevelZeroTablesStall, "vlog_file_bytes": effective.ValueLogFileSize,
					"value_threshold": effective.ValueThreshold, "block_cache_bytes": effective.BlockCacheSize,
					"index_cache_bytes": effective.IndexCacheSize, "compression": effective.Compression,
					"collect_window_ns": batch.CollectWindow.Nanoseconds(), "max_batch": batch.MaxBatch,
				}
				a := &confirmationLoadTarget{}
				if mode == "plain_no_sync" {
					err = store.UseWriteBehind(a)
				} else {
					err = store.UseWriteBehindWithFailureHandler(a, a)
				}
				require.NoError(t, err)
				var before, after syscall.Rusage
				require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &before))
				var memBefore, memAfter runtime.MemStats
				runtime.ReadMemStats(&memBefore)
				beforeVersion := store.db.manager.db.MaxVersion()
				var next, failures atomic.Int64
				var wg sync.WaitGroup
				start := time.Now()
				_ = start
				latencies := make([][]float64, 50)
				pendingTrend := []int{}
				var trendMu sync.Mutex
				for worker := range 50 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for {
							i := next.Add(1) - 1
							if round < 3 && i >= 2000 || round == 3 && i >= 40000 {
								return
							}
							if round == 3 {
								if wait := time.Until(start.Add(time.Duration(i/50) * 25 * time.Millisecond)); wait > 0 {
									time.Sleep(wait)
								}
							}
							key := i
							if round == 3 {
								key = i % 2000
							} // 持续窗口重复工作集；无后台组不因10000硬限产生伪性能错误。
							item := &confirmationLoadRecord{Model: entity.NewModel(), Key: fmt.Sprint(key), Group: int(key % 50), Payload: payload}
							began := time.Now()
							e := store.Save(context.Background(), item)
							latencies[worker] = append(latencies[worker], float64(time.Since(began))/float64(time.Millisecond))
							if e != nil {
								failures.Add(1)
							}
							if i%200 == 0 {
								trendMu.Lock()
								pendingTrend = append(pendingTrend, store.Metrics().Pending)
								trendMu.Unlock()
							}
						}
					}()
				}
				wg.Wait()
				writeTime := time.Since(start)
				atWrite := store.Metrics()
				drainStart := time.Now()
				if cfg.AutoSync {
					require.Eventually(t, func() bool { m := store.Metrics(); return m.Pending == 0 && (round == 3 || m.Sync.SyncedItems == 2000) }, 30*time.Second, time.Millisecond)
				}
				endMetrics := store.Metrics()
				transactions := store.db.manager.db.MaxVersion() - beforeVersion
				drainTime := time.Since(drainStart)
				runtime.ReadMemStats(&memAfter)
				require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &after))
				all := []float64{}
				for _, v := range latencies {
					all = append(all, v...)
				}
				sort.Float64s(all)
				require.NotEmpty(t, all)
				sum := 0.0
				for _, v := range all {
					sum += v
				}
				pct := func(p float64) float64 { return all[int(math.Ceil(p*float64(len(all))))-1] }
				result := map[string]any{"variant": variant, "effective_options": effectiveOptions, "levels_after_drain": store.db.manager.db.Levels(), "mode": mode, "round": round, "sustained": round == 3, "fixed_working_set": round == 3, "sustained_offered_per_second": 2000, "payload_bytes": len(payload), "gomaxprocs": runtime.GOMAXPROCS(0), "sync_writes": cfg.SyncWrites, "completed": len(all), "errors": failures.Load(), "write_seconds": writeTime.Seconds(), "drain_seconds": drainTime.Seconds(), "save_ms": map[string]float64{"avg": sum / float64(len(all)), "p50": pct(.5), "p95": pct(.95), "p99": pct(.99), "max": all[len(all)-1]}, "at_write_end": atWrite, "after_drain": endMetrics, "pending_trend": pendingTrend, "target_calls": a.calls.Load(), "target_items": a.items.Load(), "target_duration_ns": a.nanos.Load(), "badger_committed_transactions": transactions, "cpu_user_seconds": float64(after.Utime.Nano()-before.Utime.Nano()) / 1e9, "cpu_system_seconds": float64(after.Stime.Nano()-before.Stime.Nano()) / 1e9, "max_rss_native_units": after.Maxrss, "gc_count": memAfter.NumGC - memBefore.NumGC, "gc_pause_ns": memAfter.PauseTotalNs - memBefore.PauseTotalNs, "heap_bytes_end": memAfter.HeapAlloc, "fsync_msync_calls": nil, "cpu_throttling": nil, "io_syscalls": nil, "pure_fsync_duration": nil, "hard_cpu_memory_limits": false}
				// 在测量窗口外核对持久原件和索引，不能仅凭缓存pending=0宣称同步成功。
				persistentPending, err := store.db.GetPendingSyncCount()
				require.NoError(t, err)
				require.Equal(t, endMetrics.Pending, persistentPending)
				verifiedSynced := 0
				for key := range 2000 {
					wrapper, err := store.db.getWrapper(store.db.prefix + fmt.Sprint(key))
					require.NoError(t, err)
					require.NotNil(t, wrapper.Item)
					require.Equal(t, fmt.Sprint(key), wrapper.Item.Key)
					require.Equal(t, key%50, wrapper.Item.Group)
					require.True(t, bytes.Equal(payload, wrapper.Item.Payload), "合成载荷须逐件匹配")
					require.False(t, wrapper.IsDeleted)
					require.Equal(t, cfg.AutoSync, wrapper.IsSynced)
					if wrapper.IsSynced {
						verifiedSynced++
					}
				}
				result["verified_local_keys"] = 2000
				result["verified_synced_keys"] = verifiedSynced
				result["persistent_pending"] = persistentPending
				result["visible_logical_cpus"] = runtime.NumCPU()
				result["gomaxprocs_env"] = os.Getenv("GOMAXPROCS")
				result["gomemlimit_env"] = os.Getenv("GOMEMLIMIT")
				closeErr := store.Close(context.Background())
				if cfg.AutoSync {
					require.NoError(t, closeErr)
				} else {
					var pending *PendingSyncError
					require.True(t, errors.As(closeErr, &pending))
					require.Greater(t, endMetrics.Pending, 0)
				}
				require.NoError(t, CloseSharedManager(store.config.BasePath))
				require.Zero(t, failures.Load())
				b, err := json.MarshalIndent(result, "", "  ")
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(path, "result.json"), b, 0600))
				t.Logf("PERF_RESULT %s", b)
				runtime.GC()
			})
		}
	}
}
