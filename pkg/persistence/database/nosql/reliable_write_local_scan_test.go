// Package nosql 验证可靠门面有界续页、恢复与失败边界，使用真实临时 Badger。
package nosql

import (
	"context"
	"encoding/base64"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"testing"

	"github.com/stretchr/testify/require"
)

type fundLocalPageScanner interface {
	ScanLocalPage(context.Context, string, string, int) ([]*testFund, string, error)
}

// TestReliableLocalScanPageContract 先可靠写入真实原件，再验证公开门面的续页合同；旧正式版缺少此能力为 RED。
func TestReliableLocalScanPageContract(t *testing.T) {
	store, _, err := NewReliableWriteStore[testFund](testReliableIdentity(), testReliableConfig(t))
	require.NoError(t, err)
	cleanupReliableStore(t, store)
	require.NoError(t, store.UseWriteBehind(confirmAllFundTarget{}))
	items := []*testFund{newFund("page-a", "HK", 1), newFund("page-b", "HK", 2), newFund("page-c", "HK", 3)}
	result, err := store.SaveBatch(context.Background(), items)
	require.NoError(t, err)
	require.Equal(t, 3, result.Committed)
	legacy, err := store.ScanLocal(context.Background(), LocalScanOptions{Limit: 0})
	require.NoError(t, err)
	require.Len(t, legacy, 3)
	scanner, ok := any(store).(fundLocalPageScanner)
	require.True(t, ok, "LOCAL_SCAN_PAGE_MISSING: 真实原件已保存，ReliableWriteStore 没有可续页的有界公开读取")
	seen := map[string]bool{}
	cursor := ""
	for {
		page, next, err := scanner.ScanLocalPage(context.Background(), "", cursor, 1)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page), 1)
		for _, item := range page {
			require.False(t, seen[item.GetHash()])
			seen[item.GetHash()] = true
		}
		if next == "" {
			break
		}
		require.NotEqual(t, cursor, next)
		cursor = next
	}
	require.Len(t, seen, 3)
	require.Equal(t, 3, store.Metrics().Pending)
}

// scanPageRecord 只在真实临时卷中提供稳定 key 和可控载荷，不承载消费方业务语义。
type scanPageRecord struct {
	*entity.Model
	Key  string
	Data string
}

var scanPageDecodeHook atomic.Pointer[func()]

// GetHash 返回测试原件的稳定本地 key。
func (r *scanPageRecord) GetHash() string { return r.Key }

// NewModel 初始化模型，并允许测试在实际解码点建立同步屏障。
func (r *scanPageRecord) NewModel() {
	if r.Model == nil {
		r.Model = entity.NewModel()
	}
	if hook := scanPageDecodeHook.Load(); hook != nil {
		(*hook)()
	}
}

type scanPageTarget struct{}

// SyncBatch 仅返回测试原件的确认集合，不访问外部系统。
func (scanPageTarget) SyncBatch(_ context.Context, items []*SyncQueueItem[scanPageRecord]) (*WriteBehindResult, error) {
	result := &WriteBehindResult{}
	for _, item := range items {
		result.ConfirmedKeys = append(result.ConfirmedKeys, item.Key)
	}
	return result, nil
}

func openScanPageStore(t *testing.T, configs ...ReliableWriteStoreConfig) *ReliableWriteStore[scanPageRecord] {
	t.Helper()
	cfg := testReliableConfig(t)
	if len(configs) > 0 {
		cfg = configs[0]
	}
	store, _, err := NewReliableWriteStore[scanPageRecord](testReliableIdentity(), cfg)
	require.NoError(t, err)
	require.True(t, store.db.manager.db.Opts().SyncWrites)
	require.True(t, store.db.manager.db.Opts().DetectConflicts)
	require.NoError(t, store.UseWriteBehind(scanPageTarget{}))
	t.Cleanup(func() {
		_ = store.Close(context.Background())
		require.NoError(t, CloseSharedManager(store.config.BasePath))
	})
	return store
}

func saveScanPageRecords(t *testing.T, store *ReliableWriteStore[scanPageRecord], items ...*scanPageRecord) {
	t.Helper()
	result, err := store.SaveBatch(context.Background(), items)
	require.NoError(t, err)
	require.Equal(t, len(items), result.Committed)
}

func scanRecord(key, data string) *scanPageRecord {
	return &scanPageRecord{Model: entity.NewModel(), Key: key, Data: data}
}

// TestReliableLocalScanPageValidation 验证正 limit、固定上限、空范围、游标版本和 store/模型/前缀绑定。
func TestReliableLocalScanPageValidation(t *testing.T) {
	store := openScanPageStore(t)
	saveScanPageRecords(t, store, scanRecord("a/1", "one"), scanRecord("a/2", "two"), scanRecord("b/1", "other"))
	page, cursor, err := store.ScanLocalPage(nil, "a/", "", 1)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.NotEmpty(t, cursor)
	for _, limit := range []int{-1, 0, LocalScanPageMaxItems + 1} {
		page, next, err := store.ScanLocalPage(nil, "a/", "", limit)
		require.ErrorIs(t, err, ErrInvalidLocalScanPage)
		require.Nil(t, page)
		require.Empty(t, next)
	}
	for _, bad := range []string{"not-a-cursor", strings.Repeat("a", localScanCursorMaxLength+1), base64.RawURLEncoding.EncodeToString([]byte{2, 0})} {
		_, _, err := store.ScanLocalPage(nil, "a/", bad, 1)
		require.ErrorIs(t, err, ErrInvalidLocalScanPage)
	}
	_, _, err = store.ScanLocalPage(nil, "b/", cursor, 1)
	require.ErrorIs(t, err, ErrInvalidLocalScanPage)
	other := openScanPageStore(t)
	_, _, err = other.ScanLocalPage(nil, "a/", cursor, 1)
	require.ErrorIs(t, err, ErrInvalidLocalScanPage)
	modelStore, _, err := NewReliableWriteStore[testFund](testReliableIdentity(), ReliableWriteStoreConfig{BasePath: filepath.Dir(filepath.Dir(filepath.Dir(store.config.BasePath))), Badger: store.config.Badger})
	require.NoError(t, err)
	defer modelStore.Close(context.Background())
	_, _, err = modelStore.ScanLocalPage(nil, "a/", cursor, 1)
	require.ErrorIs(t, err, ErrInvalidLocalScanPage)
	empty, next, err := store.ScanLocalPage(nil, "missing/", "", 1)
	require.NoError(t, err)
	require.Empty(t, empty)
	require.Empty(t, next)
	// 篡改游标的物理位置到范围外，即使保留 binding 仍明确拒绝。
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	require.NoError(t, err)
	raw = append(raw[:33], []byte("outside")...)
	_, _, err = store.ScanLocalPage(nil, "a/", base64.RawURLEncoding.EncodeToString(raw), 1)
	require.ErrorIs(t, err, ErrInvalidLocalScanPage)
}

// TestReliableLocalScanPageDeletedWindow 验证满预算 tombstone 空页仍推进，且范围末尾不会进入相邻模型/前缀。
func TestReliableLocalScanPageDeletedWindow(t *testing.T) {
	store := openScanPageStore(t)
	for start := 0; start < LocalScanPageMaxVisitedKeys; start += 128 {
		items := make([]*scanPageRecord, 0, 128)
		for i := start; i < start+128; i++ {
			items = append(items, scanRecord(fmt.Sprintf("a/%05d", i), "deleted"))
		}
		saveScanPageRecords(t, store, items...)
		result, err := store.DeleteBatch(context.Background(), items)
		require.NoError(t, err)
		require.Equal(t, 128, result.Committed)
	}
	saveScanPageRecords(t, store, scanRecord("a/z", "live"), scanRecord("b/other", "out-of-range"))
	before := store.Metrics().Pending
	page, cursor, err := store.ScanLocalPage(nil, "a/", "", 10)
	require.NoError(t, err)
	require.Empty(t, page)
	require.NotEmpty(t, cursor)
	binding, err := store.localScanBinding("a/")
	require.NoError(t, err)
	last, err := decodeLocalScanCursor(cursor, binding, []byte(store.db.prefix+"a/"))
	require.NoError(t, err)
	require.Equal(t, store.db.prefix+fmt.Sprintf("a/%05d", LocalScanPageMaxVisitedKeys-1), string(last))
	page, next, err := store.ScanLocalPage(nil, "a/", cursor, 10)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, "a/z", page[0].Key)
	require.Empty(t, next)
	require.Equal(t, before, store.Metrics().Pending)
}

// TestReliableLocalScanPageByteBudget 验证实际编码字节限制、单大项失败不跳过，以及解码错误使整页失败。
func TestReliableLocalScanPageByteBudget(t *testing.T) {
	// 单项大于8MiB必须先能按正式写API落盘；默认1MiB vlog拒绝该fixture，故仅此边界用合法大vlog。
	cfg := testReliableConfig(t)
	cfg.Badger.ValueLogFileSize = 32 << 20
	cfg.Badger.MemTableSize = 128 << 20
	store := openScanPageStore(t, cfg)
	for _, key := range []string{"a/1", "a/2", "a/3"} {
		saveScanPageRecords(t, store, scanRecord(key, strings.Repeat("x", 3<<20)))
	}
	page, cursor, err := store.ScanLocalPage(nil, "a/", "", 100)
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.NotEmpty(t, cursor)
	page, next, err := store.ScanLocalPage(nil, "a/", cursor, 100)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Empty(t, next)
	saveScanPageRecords(t, store, scanRecord("oversize/1", strings.Repeat("x", LocalScanPageMaxBytes)))
	page, next, err = store.ScanLocalPage(nil, "oversize/", "", 1)
	require.ErrorIs(t, err, ErrLocalScanItemTooLarge)
	require.Nil(t, page)
	require.Empty(t, next)
	// 只在隔离卷中构造损坏原件：不能日志后跳过，也不能交付前半页的可继续游标。
	require.NoError(t, store.db.manager.db.Update(func(txn *badger.Txn) error { return txn.Set([]byte(store.db.prefix+"a/2"), []byte("invalid-json")) }))
	page, next, err = store.ScanLocalPage(nil, "a/", "", 100)
	require.ErrorIs(t, err, ErrLocalScanData)
	require.Nil(t, page)
	require.Empty(t, next)
	require.NoError(t, store.db.manager.db.Update(func(txn *badger.Txn) error { return txn.Set([]byte(store.db.prefix+"a/2"), []byte(`{"Item":null}`)) }))
	_, _, err = store.ScanLocalPage(nil, "a/", "", 100)
	require.ErrorIs(t, err, ErrLocalScanData)
}

// TestReliableLocalScanPageCancelAndClose 用解码屏障而非时间阈值验证扫描中取消与关闭，不新增扫描 worker。
func TestReliableLocalScanPageCancelAndClose(t *testing.T) {
	for _, mode := range []string{"cancel", "close"} {
		t.Run(mode, func(t *testing.T) {
			store := openScanPageStore(t)
			saveScanPageRecords(t, store, scanRecord("a", "one"), scanRecord("b", "two"))
			entered, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			hook := func() {
				if calls.Add(1) == 1 {
					close(entered)
					<-release
				}
			}
			scanPageDecodeHook.Store(&hook)
			t.Cleanup(func() { scanPageDecodeHook.Store(nil) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				page, next, err := store.ScanLocalPage(ctx, "", "", 10)
				if page != nil || next != "" {
					done <- fmt.Errorf("失败仍返回可继续结果")
					return
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("未进入实际解码屏障")
			}
			expected := error(context.Canceled)
			if mode == "cancel" {
				cancel()
			} else {
				_ = store.Close(context.Background())
				expected = ErrWriteStoreClosed
			}
			close(release)
			select {
			case err := <-done:
				require.ErrorIs(t, err, expected)
			case <-time.After(5 * time.Second):
				t.Fatal("扫描没有退出")
			}
			require.EqualValues(t, 1, calls.Load())
		})
	}
}

// TestReliableLocalScanPageConcurrentChanges 明确每页新快照：删除游标项仍续页，但新插入游标后方才可见，不声称跨页快照。
func TestReliableLocalScanPageConcurrentChanges(t *testing.T) {
	store := openScanPageStore(t)
	saveScanPageRecords(t, store, scanRecord("a", "one"), scanRecord("c", "three"))
	page, cursor, err := store.ScanLocalPage(nil, "", "", 1)
	require.NoError(t, err)
	require.Equal(t, "a", page[0].Key)
	require.NoError(t, store.Delete(context.Background(), page[0]))
	saveScanPageRecords(t, store, scanRecord("0", "behind"), scanRecord("b", "ahead"), scanRecord("c", "updated"))
	page, next, err := store.ScanLocalPage(nil, "", cursor, 100)
	require.NoError(t, err)
	require.Empty(t, next)
	require.Len(t, page, 2)
	require.Equal(t, "b", page[0].Key)
	require.Equal(t, "updated", page[1].Data)
}

// TestReliableLocalScanPageProcess 由父测试在独立进程创建和重开同一可靠卷，游标不依赖内存 session。
func TestReliableLocalScanPageProcess(t *testing.T) {
	mode := os.Getenv("CORE_LOCAL_SCAN_PROCESS_MODE")
	if mode == "" {
		t.Skip("仅供分页进程恢复父测试启动")
	}
	root := os.Getenv("CORE_LOCAL_SCAN_PROCESS_ROOT")
	cfg := testReliableConfig(t)
	cfg.BasePath = filepath.Join(root, "store")
	store, _, err := NewReliableWriteStore[scanPageRecord](testReliableIdentity(), cfg)
	require.NoError(t, err)
	require.NoError(t, store.UseWriteBehind(scanPageTarget{}))
	if mode == "read" {
		cursor, err := os.ReadFile(filepath.Join(root, "cursor"))
		require.NoError(t, err)
		page, next, err := store.ScanLocalPage(nil, "", string(cursor), 100)
		require.NoError(t, err)
		require.Empty(t, next)
		require.Len(t, page, 4)
		require.Equal(t, "key-3", page[0].Key)
		pending, err := store.db.GetPendingSyncCount()
		require.NoError(t, err)
		require.Equal(t, 7, pending, "核对持久索引，不能把旧模式重开后的未初始化缓存当持久pending")
	} else {
		for i := 0; i < 7; i++ {
			saveScanPageRecords(t, store, scanRecord(fmt.Sprintf("key-%d", i), "payload"))
		}
		page, cursor, err := store.ScanLocalPage(nil, "", "", 3)
		require.NoError(t, err)
		require.Len(t, page, 3)
		require.NotEmpty(t, cursor)
		require.NoError(t, os.WriteFile(filepath.Join(root, "cursor"), []byte(cursor), 0600))
		if mode == "hold" {
			require.NoError(t, os.WriteFile(filepath.Join(root, "ready"), []byte("ready"), 0600))
			select {}
		}
	}
	_ = store.Close(context.Background())
	require.NoError(t, CloseSharedManager(store.config.BasePath))
}

// TestReliableLocalScanPageRestart 验证正常关闭和真实 SIGKILL 后，新 PID 无新 Save 即可重开原目录续页。
func TestReliableLocalScanPageRestart(t *testing.T) {
	for _, mode := range []string{"seed", "hold"} {
		t.Run(mode, func(t *testing.T) {
			root, err := os.MkdirTemp("", "core-local-page-process-")
			require.NoError(t, err)
			t.Cleanup(func() {
				if t.Failed() {
					t.Logf("保留失败卷: %s", root)
				} else {
					require.NoError(t, os.RemoveAll(root))
				}
			})
			exe, err := os.Executable()
			require.NoError(t, err)
			start := func(mode string) *exec.Cmd {
				cmd := exec.Command(exe, "-test.run=^TestReliableLocalScanPageProcess$", "-test.v", "-test.timeout=30s")
				cmd.Env = append(os.Environ(), "CORE_LOCAL_SCAN_PROCESS_MODE="+mode, "CORE_LOCAL_SCAN_PROCESS_ROOT="+root)
				log, err := os.Create(filepath.Join(root, mode+".log"))
				require.NoError(t, err)
				t.Cleanup(func() { log.Close() })
				cmd.Stdout = log
				cmd.Stderr = log
				require.NoError(t, cmd.Start())
				t.Cleanup(func() { _ = cmd.Process.Kill() })
				return cmd
			}
			writer := start(mode)
			if mode == "hold" {
				require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(root, "ready")); return err == nil }, 10*time.Second, time.Millisecond)
				require.NoError(t, writer.Process.Kill())
				require.Error(t, writer.Wait())
			} else {
				require.NoError(t, writer.Wait())
			}
			reader := start("read")
			require.NotEqual(t, writer.Process.Pid, reader.Process.Pid)
			err = reader.Wait()
			if err != nil {
				b, _ := os.ReadFile(filepath.Join(root, "read.log"))
				t.Logf("子进程: %s", b)
			}
			require.NoError(t, err)
			t.Logf("可靠刷盘、原目录续页: mode=%s writer=%d reader=%d", mode, writer.Process.Pid, reader.Process.Pid)
		})
	}
}

// TestReliableLocalScanPageMeasurement 显式运行的同机旧全量API/分页API对照；不当作消费方恢复或容量认证。
func TestReliableLocalScanPageMeasurement(t *testing.T) {
	mode := os.Getenv("CORE_LOCAL_SCAN_MEASURE")
	if mode != "legacy" && mode != "page" {
		t.Skip("仅显式本地读取对照运行")
	}
	store := openScanPageStore(t)
	const count = 4096
	for start := 0; start < count; start += 128 {
		items := make([]*scanPageRecord, 0, 128)
		for i := start; i < start+128; i++ {
			items = append(items, scanRecord(fmt.Sprintf("measurement/%05d", i), strings.Repeat("x", 2048)))
		}
		saveScanPageRecords(t, store, items...)
	}
	t.Logf("OPTIONS SyncWrites=%v DetectConflicts=%v GOMAXPROCS=%d records=%d payload=%d page_limit=128", store.db.manager.db.Opts().SyncWrites, store.db.manager.db.Opts().DetectConflicts, runtime.GOMAXPROCS(0), count, 2048)
	for round := 0; round < 3; round++ {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		started := time.Now()
		cursor := ""
		seen, calls, maxItems := 0, 0, 0
		durations := []float64{}
		var hold []*scanPageRecord
		for {
			callStart := time.Now()
			var page []*scanPageRecord
			var next string
			var err error
			if mode == "legacy" {
				page, err = store.ScanLocal(context.Background(), LocalScanOptions{Prefix: "measurement/", Limit: 0})
			} else {
				page, next, err = store.ScanLocalPage(context.Background(), "measurement/", cursor, 128)
			}
			durations = append(durations, float64(time.Since(callStart))/float64(time.Millisecond))
			require.NoError(t, err)
			calls++
			seen += len(page)
			if len(page) > maxItems {
				maxItems = len(page)
			}
			for _, item := range page {
				require.Len(t, item.Data, 2048)
			}
			hold = page
			if next == "" {
				break
			}
			cursor = next
		}
		elapsed := time.Since(started)
		require.Equal(t, count, seen)
		runtime.GC()
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(hold)
		sort.Float64s(durations)
		percentile := func(p float64) float64 { return durations[int(float64(len(durations)-1)*p)] }
		result := map[string]any{"mode": mode, "round": round + 1, "records": seen, "calls": calls, "max_returned_items": maxItems, "sweep_ms": float64(elapsed) / float64(time.Millisecond), "call_p50_ms": percentile(.5), "call_p95_ms": percentile(.95), "call_p99_ms": percentile(.99), "call_max_ms": durations[len(durations)-1], "total_alloc_delta": after.TotalAlloc - before.TotalAlloc, "heap_retained_delta": int64(after.HeapAlloc) - int64(before.HeapAlloc), "pending": store.Metrics().Pending}
		encoded, err := stdjson.Marshal(result)
		require.NoError(t, err)
		t.Logf("LOCAL_SCAN_MEASURE %s", encoded)
	}
	require.Equal(t, count, store.Metrics().Pending)
}

// TestReliableLocalScanPageReadOnlyBounded 验证有界失败卷的完成、暂停及pending原件均可读取，但状态、预算和hook不变。
func TestReliableLocalScanPageReadOnlyBounded(t *testing.T) {
	target := &cleanupTestTarget{}
	store := openCleanupTestStore(t, t.TempDir(), false, true, target)
	probe := &cleanupTestProbe{}
	require.NoError(t, store.Save(context.Background(), newCleanupTestRecord(t, "completed", 1, false, probe)))
	result, err := store.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, err)
	require.Equal(t, 1, result.Confirmed)
	require.Eventually(t, func() bool { return probe.calls.Load() == 1 }, time.Second, time.Millisecond)
	target.sync = func(context.Context, []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		return nil, errors.New("测试明确暂停")
	}
	require.NoError(t, store.Save(context.Background(), newCleanupTestRecord(t, "paused", 1, false, probe)))
	_, err = store.ForceSyncBatch(context.Background(), 16)
	require.Error(t, err)
	require.Eventually(t, func() bool {
		records, _, e := store.ListWriteBehindFailures("", 10)
		if e != nil {
			return false
		}
		for _, r := range records {
			if r.State == WriteBehindPaused {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond)
	require.NoError(t, store.Save(context.Background(), newCleanupTestRecord(t, "pending", 1, false, probe)))
	records, _, err := store.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	before, err := stdjson.Marshal(records)
	require.NoError(t, err)
	cursor := ""
	keys := map[string]bool{}
	for {
		page, next, err := store.ScanLocalPage(context.Background(), "", cursor, 1)
		require.NoError(t, err)
		for _, item := range page {
			keys[item.Key] = true
		}
		if next == "" {
			break
		}
		cursor = next
	}
	require.Equal(t, map[string]bool{"completed": true, "paused": true, "pending": true}, keys)
	records, _, err = store.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	after, err := stdjson.Marshal(records)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
	require.EqualValues(t, 1, probe.calls.Load())
	pending, err := store.db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 2, pending)
}

// TestReliableLocalScanPageConcurrentSave 用实际读事务内屏障证明并发Save持续提交，本页仍读原快照。
func TestReliableLocalScanPageConcurrentSave(t *testing.T) {
	store := openScanPageStore(t)
	saveScanPageRecords(t, store, scanRecord("a", "old-a"), scanRecord("c", "old-c"))
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	hook := func() {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
	}
	scanPageDecodeHook.Store(&hook)
	t.Cleanup(func() { scanPageDecodeHook.Store(nil) })
	done := make(chan []*scanPageRecord, 1)
	errs := make(chan error, 1)
	go func() { page, _, err := store.ScanLocalPage(nil, "", "", 10); done <- page; errs <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("未进入读取事务")
	}
	saved := make(chan error, 1)
	go func() {
		_, err := store.SaveBatch(context.Background(), []*scanPageRecord{scanRecord("a", "new-a"), scanRecord("b", "new-b")})
		saved <- err
	}()
	select {
	case err := <-saved:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("前台Save被只读扫描阻塞")
	}
	close(release)
	select {
	case page := <-done:
		require.NoError(t, <-errs)
		require.Len(t, page, 2)
		require.Equal(t, "old-a", page[0].Data)
		require.Equal(t, "old-c", page[1].Data)
	case <-time.After(5 * time.Second):
		t.Fatal("扫描未结束")
	}
}
