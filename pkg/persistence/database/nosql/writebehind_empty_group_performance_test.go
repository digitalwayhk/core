// 本文件显式对照同组删除历史下的真实迁移成本；准备期不计入迁移时间，不接触消费方。
package nosql

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"syscall"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

// TestEmptyGroupMigrationPerformance 使用真实Save/target确认制造组内多版本/删除历史，固定资源做三轮对照。
func TestEmptyGroupMigrationPerformance(t *testing.T) {
	if os.Getenv("CORE_EMPTY_GROUP_PERF") != "1" {
		t.Skip("显式迁移性能对照，未启用不能视为通过")
	}
	runtime.GOMAXPROCS(1)
	for round := 0; round < 3; round++ {
		t.Run(fmt.Sprint(round), func(t *testing.T) {
			s := groupOperationsStore(t, t.TempDir(), false)
			const count = 2048
			for revision := 0; revision < 2; revision++ {
				for start := 0; start < count; start += 128 {
					items := make([]*confirmationLoadRecord, 128)
					for i := range items {
						items[i] = groupOperationRecord(fmt.Sprint(start+i), 1, 512+revision)
					}
					result, err := s.SaveBatch(context.Background(), items)
					require.NoError(t, err)
					require.Equal(t, 128, result.Committed)
				}
			}
			// 固定旧读快照，确保测试期间组内历史不会被原生压缩提前移除；不是消费方用法要求。
			pinned := s.db.manager.db.NewTransaction(false)
			defer pinned.Discard()
			_, err := s.ForceSyncAll(context.Background())
			require.NoError(t, err)
			require.Zero(t, s.Metrics().Pending)
			requireDispositionGroup(t, s.db, 1, false)
			require.NoError(t, s.db.updateWriteBehindTransaction(func(txn *badger.Txn) error {
				if err := dispositionWrite(txn, s.db.dispositionPrefix()+"history-init", ""); err != nil {
					return err
				}
				return dispositionWrite(txn, s.db.dispositionPrefix()+"history-batches-init", "")
			}))
			checks := 0
			s.db.disposition.groupCheckObserver = func(_ *badger.Txn, opts badger.IteratorOptions) {
				require.Equal(t, s.db.dispositionPrefix()+"q:"+dispositionHash("group:1")+":", string(opts.Prefix))
				checks++
			}
			dir := os.Getenv("CORE_EMPTY_GROUP_PROFILE_DIR")
			if dir != "" {
				require.NoError(t, os.MkdirAll(dir, 0700))
				file, err := os.Create(filepath.Join(dir, fmt.Sprintf("migration-%d.cpu", round)))
				require.NoError(t, err)
				require.NoError(t, pprof.StartCPUProfile(file))
				defer func() { pprof.StopCPUProfile(); _ = file.Close() }()
			}
			before := s.db.GetSyncMetrics().MetadataLocal
			var cpuBefore, cpuAfter syscall.Rusage
			require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &cpuBefore))
			start := time.Now()
			err = s.db.initializeTerminalHistory()
			elapsed := time.Since(start)
			require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &cpuAfter))
			require.NoError(t, err)
			after := s.db.GetSyncMetrics().MetadataLocal
			usage, err := s.WriteBehindHistoryUsage(context.Background())
			require.NoError(t, err)
			require.Equal(t, int64(2*count), usage.TerminalRecords)
			require.Equal(t, int64(count), usage.BusinessRecords)
			require.Zero(t, usage.Pending)
			cpu := func(r syscall.Rusage) float64 {
				return float64(r.Utime.Sec+r.Stime.Sec) + float64(r.Utime.Usec+r.Stime.Usec)/1e6
			}
			report := map[string]any{"round": round, "gomaxprocs": runtime.GOMAXPROCS(0), "sync_writes": s.db.manager.config.SyncWrites, "detect_conflicts": s.db.manager.config.DetectConflicts, "value_threshold": s.db.manager.config.ValueThreshold, "terminal_records": usage.TerminalRecords, "business_records": usage.BusinessRecords, "pending": usage.Pending, "iterator_seeks": checks, "wall_seconds": elapsed.Seconds(), "cpu_seconds": cpu(cpuAfter) - cpu(cpuBefore), "metadata_transactions": after.Committed - before.Committed, "metadata_queue_wait_seconds": (after.QueueWait - before.QueueWait).Seconds(), "metadata_lock_wait_seconds": (after.LockWait - before.LockWait).Seconds(), "metadata_transaction_seconds": (after.CommitDuration - before.CommitDuration).Seconds(), "errors": 0, "physical_io_bytes": nil, "fsync_calls": nil, "note": "固定旧读快照保留组内MVCC与删除历史；CPU累计不是墙钟，嵌套阶段不可相加"}
			raw, err := json.Marshal(report)
			require.NoError(t, err)
			t.Log(string(raw))
		})
	}
}
