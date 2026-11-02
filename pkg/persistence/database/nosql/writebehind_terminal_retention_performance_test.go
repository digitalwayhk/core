// 本文件在同机真实文件系统比较终态保留修复前后；仅显式启用，不代表消费方容量。
package nosql

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

// TestTerminalRetentionPerformance 使用固定50并发、约2KiB模型、可靠刷盘、1ms合批，多轮持续输入。
func TestTerminalRetentionPerformance(t *testing.T) {
	if os.Getenv("CORE_RETENTION_PERF") != "1" {
		t.Skip("显式性能实验")
	}
	rounds := 3
	seconds := 3
	rate := 0
	if v, e := strconv.Atoi(os.Getenv("CORE_RETENTION_PERF_RATE")); e == nil && v > 0 && v <= 10000 {
		rate = v
	}
	if v, e := strconv.Atoi(os.Getenv("CORE_RETENTION_PERF_SECONDS")); e == nil && v > 0 && v <= 300 {
		seconds = v
	}
	if v, e := strconv.Atoi(os.Getenv("CORE_RETENTION_PERF_ROUNDS")); e == nil && v > 0 && v <= 3 {
		rounds = v
	}
	modes := []string{"plain", "bounded_no_sync", "bounded_fast_sync"}
	if os.Getenv("CORE_RETENTION_PERF_MODE") == "bounded_fast_sync" {
		modes = []string{"bounded_fast_sync"}
	}
	for round := 0; round < rounds; round++ {
		for _, mode := range modes {
			t.Run(fmt.Sprintf("%s/%d", mode, round), func(t *testing.T) {
				base := t.TempDir()
				cfg := DefaultProductionConfig(base)
				cfg.AutoSync = mode == "bounded_fast_sync"
				cfg.EnableLogger = false
				cfg.SyncBatchSize = 128
				cfg.SyncBacklogDrainDelay = 0
				if mode != "plain" {
					cfg.WriteBehindFailure = failureTestPolicy()
					cfg.WriteBehindFailure.ScanLimit = 128
				}
				s, _, openErr := NewReliableWriteStore[cleanupTestRecord](ServiceIdentity{ServiceName: "retention-perf", DataCenterID: 1, MachineID: 1}, ReliableWriteStoreConfig{BasePath: base, Badger: cfg, Batch: BatchCommitConfig{MaxBatch: 128, CollectBacklog: 1, CollectWindow: time.Millisecond, QueueCapacity: 1024}, Admission: WriteAdmissionConfig{MaxConcurrent: 128, AcquireTimeout: 2 * time.Second}, CloseTimeout: time.Second})
				require.NoError(t, openErr)
				t.Cleanup(func() { _ = s.Close(context.Background()); _ = CloseSharedManager(s.config.BasePath) })
				target := &cleanupTestTarget{}
				if mode == "plain" {
					require.NoError(t, s.UseWriteBehind(target))
				} else {
					require.NoError(t, s.UseWriteBehindWithFailureHandler(target, target))
				}
				// 自动worker的回收入口尚未存在时也以完全相同输入测量；所有本地目录独立。
				pool := make([]string, 1024)
				rng := rand.New(rand.NewSource(420))
				alphabet := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
				for i := range pool {
					value := make([]byte, 2028)
					for j := range value {
						value[j] = alphabet[rng.Intn(len(alphabet))]
					}
					pool[i] = string(value)
				}
				var seq atomic.Int64
				var failures atomic.Int64
				var mu sync.Mutex
				var latencies []float64
				var wg sync.WaitGroup
				var memoryBefore runtime.MemStats
				runtime.ReadMemStats(&memoryBefore)
				var usageBefore syscall.Rusage
				require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &usageBefore))
				started := time.Now()
				until := started.Add(time.Duration(seconds) * time.Second)
				work := make(chan struct{}, 50)
				if rate > 0 {
					go func() {
						defer close(work)
						ticker := time.NewTicker(time.Second * 50 / time.Duration(rate))
						defer ticker.Stop()
						total := seconds * rate
						for sent := 0; sent < total; {
							if sent > 0 {
								<-ticker.C
							}
							burst := 50
							if total-sent < burst {
								burst = total - sent
							}
							for range burst {
								work <- struct{}{}
								sent++
							}
						}
					}()
				}
				for range 50 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for {
							if rate > 0 {
								if _, ok := <-work; !ok {
									return
								}
							} else if !time.Now().Before(until) {
								return
							}
							id := seq.Add(1)
							item := &cleanupTestRecord{Key: fmt.Sprintf("key-%d", id), Group: fmt.Sprintf("group-%d", id%128), ProbeID: pool[id%int64(len(pool))], Revision: 1, DeleteAfter: true}
							item.NewModel()
							begin := time.Now()
							e := s.Save(context.Background(), item)
							elapsed := float64(time.Since(begin)) / float64(time.Millisecond)
							if e != nil {
								failures.Add(1)
							}
							mu.Lock()
							latencies = append(latencies, elapsed)
							mu.Unlock()
						}
					}()
				}
				wg.Wait()
				wall := time.Since(started)
				before := s.Metrics()
				drainStarted := time.Now()
				drained := true
				if mode == "bounded_fast_sync" {
					deadline := time.Now().Add(30 * time.Second)
					for s.Metrics().Pending != 0 && time.Now().Before(deadline) {
						time.Sleep(time.Millisecond)
					}
					drained = s.Metrics().Pending == 0
				}
				drainSeconds := time.Since(drainStarted).Seconds()
				var memoryAfter runtime.MemStats
				runtime.ReadMemStats(&memoryAfter)
				var usageAfter syscall.Rusage
				require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &usageAfter))
				cpu := func(v syscall.Rusage) float64 {
					return float64(v.Utime.Sec+v.Stime.Sec) + float64(v.Utime.Usec+v.Stime.Usec)/1e6
				}
				var physicalLogical, physicalAllocated int64
				require.NoError(t, filepath.Walk(s.config.BasePath, func(_ string, info os.FileInfo, e error) error {
					if e != nil {
						return e
					}
					if info.Mode().IsRegular() {
						physicalLogical += info.Size()
						if stat, ok := info.Sys().(*syscall.Stat_t); ok {
							physicalAllocated += stat.Blocks * 512
						}
					}
					return nil
				}))
				var logical, payload, records, batchRecords int64
				var businessRecords, businessBytes, terminalPayload, terminalMetadata, unresolvedBytes int64
				require.NoError(t, s.db.manager.db.View(func(txn *badger.Txn) error {
					opts := badger.DefaultIteratorOptions
					opts.PrefetchValues = false
					opts.Prefix = []byte(s.db.dispositionPrefix())
					it := txn.NewIterator(opts)
					defer it.Close()
					for it.Seek(opts.Prefix); it.ValidForPrefix(opts.Prefix); it.Next() {
						logical += int64(it.Item().ValueSize())
						k := string(it.Item().Key())
						if strings.HasPrefix(k, s.db.dispositionPrefix()+"r:") {
							records++
							var r WriteBehindFailureRecord[cleanupTestRecord]
							if e := it.Item().Value(func(b []byte) error { return json.Unmarshal(b, &r) }); e != nil {
								return e
							}
							if r.Snapshot != nil && r.Snapshot.Item != nil {
								b, _ := json.Marshal(r.Snapshot.Item)
								payload += int64(len(b))
								if dispositionTerminal(r.State) {
									terminalPayload += int64(len(b))
								}
							}
							if dispositionTerminal(r.State) {
								v := int64(it.Item().ValueSize())
								if r.Snapshot != nil && r.Snapshot.Item != nil {
									b, _ := json.Marshal(r.Snapshot.Item)
									v -= int64(len(b))
								}
								terminalMetadata += v
							} else {
								unresolvedBytes += int64(it.Item().ValueSize())
							}
						}
						if strings.HasPrefix(k, s.db.dispositionPrefix()+"b:") {
							batchRecords++
						}
					}
					return nil
				}))
				require.NoError(t, s.db.manager.db.View(func(txn *badger.Txn) error {
					opts := badger.DefaultIteratorOptions
					opts.PrefetchValues = false
					opts.Prefix = []byte(s.db.prefix)
					it := txn.NewIterator(opts)
					defer it.Close()
					for it.Seek(opts.Prefix); it.ValidForPrefix(opts.Prefix); it.Next() {
						businessRecords++
						businessBytes += int64(it.Item().ValueSize())
					}
					return nil
				}))
				sort.Float64s(latencies)
				sum := 0.0
				for _, v := range latencies {
					sum += v
				}
				q := func(p float64) float64 { return latencies[int(float64(len(latencies)-1)*p)] }
				lsm, vlog := s.db.manager.db.Size()
				out := map[string]any{"mode": mode, "round": round, "gomaxprocs": runtime.GOMAXPROCS(0), "sync_writes": s.db.manager.config.SyncWrites, "conflicts": s.db.manager.config.DetectConflicts, "seconds": wall.Seconds(), "count": len(latencies), "errors": failures.Load(), "throughput": float64(len(latencies)) / wall.Seconds(), "avg_ms": sum / float64(len(latencies)), "p50_ms": q(.50), "p95_ms": q(.95), "p99_ms": q(.99), "max_ms": q(1), "pending_at_input_end": before.Pending, "pending_at_end": s.Metrics().Pending, "drain_seconds": drainSeconds, "disposition_value_bytes": logical, "snapshot_payload_bytes": payload, "records": records, "batch_records": batchRecords, "lsm_bytes": lsm, "vlog_bytes": vlog, "metrics": before}
				native := s.db.manager.db.Opts()
				out["native_options"] = map[string]any{"sync_writes": native.SyncWrites, "detect_conflicts": native.DetectConflicts, "memtable_bytes": native.MemTableSize, "num_memtables": native.NumMemtables, "compactors": native.NumCompactors, "l0_tables": native.NumLevelZeroTables, "l0_stall": native.NumLevelZeroTablesStall, "vlog_file_bytes": native.ValueLogFileSize, "value_threshold": native.ValueThreshold, "block_cache_bytes": native.BlockCacheSize, "index_cache_bytes": native.IndexCacheSize, "compression": native.Compression}
				out["effective_options"] = map[string]any{"badger": s.db.manager.config, "batch": s.config.Batch, "admission": s.config.Admission}
				out["payload_pattern"] = "fixed-seed-ascii-pool-1024x2028"
				out["offered_rate"] = rate
				out["burst"] = 50
				out["drained_within_budget"] = drained
				out["business_records"] = businessRecords
				out["business_value_bytes"] = businessBytes
				out["terminal_payload_bytes"] = terminalPayload
				out["terminal_metadata_value_bytes"] = terminalMetadata
				out["unresolved_record_value_bytes"] = unresolvedBytes
				out["physical_file_logical_bytes"] = physicalLogical
				out["physical_file_allocated_bytes"] = physicalAllocated
				out["process_cpu_seconds"] = cpu(usageAfter) - cpu(usageBefore)
				out["heap_inuse_bytes"] = memoryAfter.HeapInuse
				out["gc_cycles_delta"] = memoryAfter.NumGC - memoryBefore.NumGC
				out["gc_pause_ns_delta"] = memoryAfter.PauseTotalNs - memoryBefore.PauseTotalNs
				out["cpu_throttled_seconds"] = nil
				out["io_bytes"] = nil
				out["fsync_calls"] = nil
				if lsm == 0 && vlog == 0 {
					out["lsm_bytes"] = nil
					out["vlog_bytes"] = nil
					out["native_badger_size_state"] = "not_refreshed_in_window"
				}
				b, e := json.Marshal(out)
				require.NoError(t, e)
				t.Log(string(b))
				require.Zero(t, failures.Load())
				require.True(t, drained, "后台必须在原30秒预算内排空，不能丢失失败轮观测")
			})
		}
	}
}
