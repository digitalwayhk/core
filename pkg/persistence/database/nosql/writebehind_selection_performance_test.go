// 本文件扩展既有真实Badger性能夹具，以独立合成卷对照选批范围；默认SKIP。
package nosql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

// selectionHistory 通过原SaveBatch/DeleteBatch/确认/应用决策构造保留记录，不清失败历史或修改消费者卷。
func selectionHistory(t *testing.T, store *ReliableWriteStore[selectionLoadRecord], payload []byte) {
	t.Helper()
	ctx := context.Background()
	// 九千个成功删除且允许本地回收的合成原件，生成真实Badger删除历史；失败/完成历史仍保留。
	for start := 0; start < 9000; start += 128 {
		items := []*selectionLoadRecord{}
		for i := start; i < start+128 && i < 9000; i++ {
			r := selectionRecord(fmt.Sprintf("a-deleted-%05d", i), i)
			r.Cleanup = true
			r.Payload = payload
			items = append(items, r)
		}
		n, e := store.SaveBatch(ctx, items)
		require.NoError(t, e)
		require.Equal(t, len(items), n.Committed)
		n, e = store.DeleteBatch(ctx, items)
		require.NoError(t, e)
		require.Equal(t, len(items), n.Committed)
	}
	result, e := store.ForceSyncAll(ctx)
	require.NoError(t, e)
	require.Equal(t, 9000, result.Confirmed)
	require.Eventually(t, func() bool {
		store.db.sadQueueMu.Lock()
		defer store.db.sadQueueMu.Unlock()
		return len(store.db.sadQueueItems) == 0
	}, 10*time.Second, time.Millisecond)
	// 两轮九千保留原件，第二轮合法替换生成Superseded原件/queue历史，不通过裸key灌入伪状态。
	for revision := range 2 {
		for start := 0; start < 9000; start += 128 {
			items := []*selectionLoadRecord{}
			for i := start; i < start+128 && i < 9000; i++ {
				r := selectionRecord(fmt.Sprintf("z-retained-%05d", i), i%128)
				r.Payload = append(bytes.Clone(payload), byte(revision))
				items = append(items, r)
			}
			n, e := store.SaveBatch(ctx, items)
			require.NoError(t, e)
			require.Equal(t, len(items), n.Committed)
		}
	}
	// 选出128个真实组首，模拟target未知回执，并由应用明确暂停；没有伪造IsSynced/丢pending。
	selected, _, e := store.db.selectDispositionRound(128)
	require.NoError(t, e)
	require.Len(t, selected, 128)
	_, _, e = store.db.commitDispositionResults(selected, nil, errors.New("合成持久未知结果"))
	require.NoError(t, e)
	for _, r := range selected {
		d := WriteBehindDecision{ID: r.ID, Version: r.Snapshot.UpdatedAt, Fingerprint: r.Fingerprint, Action: WriteBehindPause, Scope: WriteBehindScopeKey, Outcome: WriteBehindStillUnknown}
		require.NoError(t, store.ApplyWriteBehindDecision(ctx, d))
	}
	m, e := store.WriteBehindDispositionMetrics()
	require.NoError(t, e)
	require.Equal(t, 128, m.Paused)
	require.Equal(t, 9000, store.Metrics().Pending)
	for i := range 9000 {
		_, e := store.GetLocal(ctx, fmt.Sprintf("a-deleted-%05d", i))
		require.ErrorIs(t, e, badger.ErrKeyNotFound)
		r, e := store.GetLocal(ctx, fmt.Sprintf("z-retained-%05d", i))
		require.NoError(t, e)
		require.Equal(t, i%128, r.Group)
		require.Equal(t, append(bytes.Clone(payload), byte(1)), r.Payload)
	}
}

type selectionLookupTotals struct {
	Count            uint64
	CreateNS, SeekNS int64
	MaxSeekNS        int64
	Unbounded        uint64
}

// TestDispositionSelectionPerformance 单个显式持续窗口；外部串行多轮驱动复用既有Go测试，不新建负载框架。
func TestDispositionSelectionPerformance(t *testing.T) {
	if os.Getenv("CORE_SELECTION_PERF") != "1" {
		t.Skip("显式CORE_SELECTION_PERF=1，在Core隔离合成目录测量")
	}
	mode := os.Getenv("CORE_SELECTION_MODE")
	require.Contains(t, []string{"foreground", "background", "history"}, mode)
	out := os.Getenv("CORE_SELECTION_OUTPUT")
	require.NotEmpty(t, out)
	require.NoError(t, os.MkdirAll(out, 0700))
	path, e := os.MkdirTemp(out, "synthetic-")
	require.NoError(t, e)
	target := &selectionLoadTarget{}
	store := newSelectionStore(t, path, target)
	db := store.db
	payload := confirmationPayload(t)
	if mode == "history" {
		selectionHistory(t, store, payload)
	}
	var observationMu sync.Mutex
	lookups := map[string]selectionLookupTotals{}
	rounds := []float64{}
	db.disposition.selectionObserver = func(kind string, opts badger.IteratorOptions, create, seek time.Duration) {
		observationMu.Lock()
		defer observationMu.Unlock()
		v := lookups[kind]
		v.Count++
		v.CreateNS += create.Nanoseconds()
		v.SeekNS += seek.Nanoseconds()
		if seek.Nanoseconds() > v.MaxSeekNS {
			v.MaxSeekNS = seek.Nanoseconds()
		}
		if len(opts.Prefix) == 0 {
			v.Unbounded++
		}
		lookups[kind] = v
	}
	db.disposition.selectionRoundObserver = func(d time.Duration) {
		observationMu.Lock()
		rounds = append(rounds, float64(d)/float64(time.Millisecond))
		observationMu.Unlock()
	}
	beforeMetrics := store.Metrics()
	beforeVersion := db.manager.db.MaxVersion()
	beforeCalls, beforeItems := target.calls.Load(), target.items.Load()
	if mode != "foreground" {
		db.manager.config.AutoSync = true
		db.startWriteBehindWorker()
	}
	var before, after syscall.Rusage
	require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &before))
	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	var next, failures atomic.Int64
	var wg sync.WaitGroup
	latency := make([][]float64, 50)
	trend := []int{}
	var trendMu sync.Mutex
	start := time.Now()
	for worker := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := next.Add(1) - 1
				if i >= 40000 {
					return
				}
				if d := time.Until(start.Add(time.Duration(i/50) * 25 * time.Millisecond)); d > 0 {
					time.Sleep(d)
				}
				r := selectionRecord(fmt.Sprintf("input-%03d", i%512), 100000+int(i%512%50))
				r.Payload = payload
				began := time.Now()
				err := store.Save(context.Background(), r)
				latency[worker] = append(latency[worker], float64(time.Since(began))/float64(time.Millisecond))
				if err != nil {
					failures.Add(1)
				}
				if i%1000 == 0 {
					trendMu.Lock()
					trend = append(trend, store.Metrics().Pending)
					trendMu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	writeTime := time.Since(start)
	atWrite := store.Metrics()
	drainStart := time.Now()
	expectedPending := 512
	if mode == "background" {
		expectedPending = 0
	}
	if mode == "history" {
		expectedPending = 9000
	}
	if mode != "foreground" {
		require.Eventually(t, func() bool { return store.Metrics().Pending == expectedPending }, 30*time.Second, time.Millisecond)
	}
	afterMetrics := store.Metrics()
	drainTime := time.Since(drainStart)
	transactions := db.manager.db.MaxVersion() - beforeVersion
	runtime.ReadMemStats(&memAfter)
	require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &after))
	// 先停后台再复制诊断容器，不能和采集写方竞争；有pending时Close保留原契约。
	closeErr := store.Close(context.Background())
	if expectedPending > 0 {
		var p *PendingSyncError
		require.True(t, errors.As(closeErr, &p))
	} else {
		require.NoError(t, closeErr)
	}
	observationMu.Lock()
	lookupCopy := map[string]selectionLookupTotals{}
	for k, v := range lookups {
		lookupCopy[k] = v
	}
	roundCopy := append([]float64(nil), rounds...)
	observationMu.Unlock()
	persistent, e := db.GetPendingSyncCount()
	require.NoError(t, e)
	require.Equal(t, expectedPending, persistent)
	verifiedSynced := 0
	for i := range 512 {
		w, e := db.getWrapper(db.prefix + fmt.Sprintf("input-%03d", i))
		require.NoError(t, e)
		require.Equal(t, mode != "foreground", w.IsSynced)
		require.True(t, bytes.Equal(payload, w.Item.Payload))
		require.False(t, w.IsDeleted)
		if w.IsSynced {
			verifiedSynced++
		}
	}
	// 历史模型同key多轮保存不会被新输入确认，暂停及attempt持续保留。
	if mode == "history" {
		m, e := db.GetWriteBehindDispositionMetrics()
		require.NoError(t, e)
		require.Equal(t, 128, m.Paused)
		for i := range 9000 {
			w, e := db.getWrapper(db.prefix + fmt.Sprintf("z-retained-%05d", i))
			require.NoError(t, e)
			require.False(t, w.IsSynced)
			require.Equal(t, i%128, w.Item.Group)
		}
	}
	require.NoError(t, CloseSharedManager(store.config.BasePath))
	// 使用新PID重开原目录核验，父进程对象重建不算跨进程恢复。
	exe, e := os.Executable()
	require.NoError(t, e)
	if recoveryBinary := os.Getenv("CORE_SELECTION_REOPEN_BINARY"); recoveryBinary != "" {
		exe = recoveryBinary // 仅显式兼容实验；新PID以另一候选打开同一已关闭合成卷。
	}
	cmd := exec.Command(exe, "-test.run=^TestDispositionSelectionReopenProcess$", "-test.v", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), "CORE_SELECTION_REOPEN_PATH="+path, "CORE_SELECTION_REOPEN_MODE="+mode)
	recovery, e := cmd.CombinedOutput()
	require.NoError(t, os.WriteFile(filepath.Join(out, "reopen.log"), recovery, 0600))
	require.NoError(t, e, string(recovery))
	options := db.manager.db.Opts()
	all := []float64{}
	for _, v := range latency {
		all = append(all, v...)
	}
	sort.Float64s(all)
	sort.Float64s(roundCopy)
	stats := func(v []float64) any {
		if len(v) == 0 {
			return nil
		}
		sum := 0.
		for _, x := range v {
			sum += x
		}
		return map[string]float64{"avg": sum / float64(len(v)), "p50": v[(len(v)-1)/2], "p95": v[int(float64(len(v)-1)*.95)], "p99": v[int(float64(len(v)-1)*.99)], "max": v[len(v)-1]}
	}
	diskFiles := []map[string]any{}
	allocated := int64(0)
	require.NoError(t, filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		row := map[string]any{"path": p[len(path):], "bytes": info.Size()}
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		row["sha256"] = fmt.Sprintf("%x", h.Sum(nil))
		if s, ok := info.Sys().(*syscall.Stat_t); ok {
			allocated += s.Blocks * 512
		}
		diskFiles = append(diskFiles, row)
		return nil
	}))
	result := map[string]any{"mode": mode, "completed": len(all), "errors": failures.Load(), "write_seconds": writeTime.Seconds(), "throughput": float64(len(all)) / writeTime.Seconds(), "save_ms": stats(all), "selection_round_ms_including_metadata_queue": stats(roundCopy), "selection_lookup": lookupCopy, "before": beforeMetrics, "at_write_end": atWrite, "after": afterMetrics, "pending_trend": trend, "persistent_pending": persistent, "verified_input_keys": 512, "verified_synced_input_keys": verifiedSynced, "verified_retained_keys": 0, "target_calls": target.calls.Load() - beforeCalls, "target_items": target.items.Load() - beforeItems, "actual_badger_transactions": transactions, "drain_seconds": drainTime.Seconds(), "gomaxprocs": runtime.GOMAXPROCS(0), "gomemlimit_env": os.Getenv("GOMEMLIMIT"), "cpu_user_seconds": float64(after.Utime.Nano()-before.Utime.Nano()) / 1e9, "cpu_system_seconds": float64(after.Stime.Nano()-before.Stime.Nano()) / 1e9, "max_rss_native": after.Maxrss, "heap_bytes": memAfter.HeapAlloc, "gc_count": memAfter.NumGC - memBefore.NumGC, "gc_pause_ns": memAfter.PauseTotalNs - memBefore.PauseTotalNs, "cpu_hard_quota": nil, "cpu_throttling": nil, "io_syscalls": nil, "fsync_msync_calls": nil, "pure_fsync_ns": nil, "disk_allocated_after_close": allocated, "closed_files": diskFiles, "effective_options": map[string]any{"sync_writes": options.SyncWrites, "detect_conflicts": options.DetectConflicts, "memtable_bytes": options.MemTableSize, "num_memtables": options.NumMemtables, "vlog_bytes": options.ValueLogFileSize, "value_threshold": options.ValueThreshold, "compactors": options.NumCompactors, "compression": options.Compression, "block_cache_bytes": options.BlockCacheSize, "index_cache_bytes": options.IndexCacheSize, "collect_window_ns": store.batcher.config.CollectWindow.Nanoseconds(), "max_batch": store.batcher.config.MaxBatch, "queue_capacity": store.batcher.config.QueueCapacity}}
	if mode == "history" {
		result["verified_retained_keys"] = 9000
	}
	require.Zero(t, failures.Load())
	b, e := json.MarshalIndent(result, "", "  ")
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(filepath.Join(out, "result.json"), b, 0600))
	// 仅在关闭且核对原件/状态后回收本轮独占合成目录，不清消费者或共享缓存。
	require.NoError(t, os.RemoveAll(path))
	t.Logf("SELECTION_PERF mode=%s saves=%d pending=%d", mode, len(all), persistent)
}

// TestDispositionSelectionReopenProcess 在独立PID核对同一卷，不触发新Save或手动排空来掩盖恢复。
func TestDispositionSelectionReopenProcess(t *testing.T) {
	path := os.Getenv("CORE_SELECTION_REOPEN_PATH")
	if path == "" {
		t.Skip("只由Core合成卷独立进程恢复夹具调用")
	}
	mode := os.Getenv("CORE_SELECTION_REOPEN_MODE")
	target := &selectionLoadTarget{}
	store := newSelectionStore(t, path, target)
	db := store.db
	if mode == "target" {
		records, _, err := store.ListWriteBehindFailures("", 16)
		require.NoError(t, err)
		require.Len(t, records, 3)
		var original *WriteBehindFailureRecord[selectionLoadRecord]
		for i := range records {
			if records[i].Scope == WriteBehindScopeTarget {
				original = &records[i]
			}
		}
		require.NotNil(t, original)
		require.Equal(t, WriteBehindPaused, original.State)
		require.Equal(t, 1, original.Attempts)
		require.NotEmpty(t, original.BatchID)
		require.Equal(t, 3, store.Metrics().Pending)
		selected, _, err := db.selectDispositionRound(128)
		require.NoError(t, err)
		require.Empty(t, selected, "重建缓存不得绕过原target屏障")
		require.NoError(t, store.ApplyWriteBehindDecision(context.Background(), WriteBehindDecision{ID: original.ID, Version: original.Snapshot.UpdatedAt, Fingerprint: original.Fingerprint, Action: WriteBehindConfirm, Scope: WriteBehindScopeTarget, Outcome: WriteBehindCommitted}))
		require.Equal(t, 2, store.Metrics().Pending)
		selected, _, err = db.selectDispositionRound(128)
		require.NoError(t, err)
		require.Len(t, selected, 2)
		for _, r := range selected {
			require.NotEqual(t, original.ID, r.ID)
			require.Equal(t, 1, r.Attempts)
		}
		require.Zero(t, target.calls.Load())
		t.Logf("新PID=%d，原target屏障保持；应用真实Confirm后独立健康组恢复", os.Getpid())
		return
	}
	expected := 0
	if mode == "foreground" {
		expected = 512
	}
	if mode == "history" {
		expected = 9000
	}
	pending, e := db.GetPendingSyncCount()
	require.NoError(t, e)
	require.Equal(t, expected, pending)
	require.Equal(t, expected, store.Metrics().Pending)
	for i := range 512 {
		w, e := db.getWrapper(db.prefix + fmt.Sprintf("input-%03d", i))
		require.NoError(t, e)
		require.Equal(t, mode != "foreground", w.IsSynced)
	}
	if mode == "history" {
		m, e := store.WriteBehindDispositionMetrics()
		require.NoError(t, e)
		require.Equal(t, 128, m.Paused)
		for i := range 9000 {
			w, e := db.getWrapper(db.prefix + fmt.Sprintf("z-retained-%05d", i))
			require.NoError(t, e)
			require.False(t, w.IsSynced)
			require.Equal(t, i%128, w.Item.Group)
		}
		require.NoError(t, db.manager.db.View(func(txn *badger.Txn) error {
			for i := range 128 {
				key := db.prefix + fmt.Sprintf("z-retained-%05d", i)
				var id string
				if e := dispositionRead(txn, db.dispositionPrefix()+"key:"+dispositionHash(key), &id); e != nil {
					return e
				}
				var r WriteBehindFailureRecord[selectionLoadRecord]
				if e := dispositionRead(txn, db.dispositionPrefix()+"r:"+id, &r); e != nil {
					return e
				}
				require.Equal(t, WriteBehindPaused, r.State)
				require.Equal(t, 1, r.Attempts)
				require.NotEmpty(t, r.BatchID)
			}
			return nil
		}))
	}
	require.Zero(t, target.calls.Load(), "重开核验不能重做target或重置预算")
	t.Logf("独立PID=%d mode=%s 持久pending=%d，512输入与保留状态已核对", os.Getpid(), mode, pending)
}
