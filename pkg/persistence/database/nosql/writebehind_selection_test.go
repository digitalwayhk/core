// 本文件用真实Badger核验选批范围、cursor和原FIFO；不连接消费方或远端存储。
package nosql

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type selectionLoadRecord struct {
	*entity.Model
	Key     string
	Group   int
	Cleanup bool
	Payload []byte
}

func (r *selectionLoadRecord) NewModel() {
	if r.Model == nil {
		r.Model = entity.NewModel()
	}
}
func (r *selectionLoadRecord) GetLocalKey() string     { return r.Key }
func (r *selectionLoadRecord) IsSyncAfterDelete() bool { return r.Cleanup }

type selectionLoadTarget struct {
	calls, items atomic.Int64
	sync         func(context.Context, []*SyncQueueItem[selectionLoadRecord]) (*WriteBehindResult, error)
	handle       func(context.Context, WriteBehindFailure[selectionLoadRecord]) (WriteBehindDecision, error)
}

func (a *selectionLoadTarget) SyncBatch(ctx context.Context, items []*SyncQueueItem[selectionLoadRecord]) (*WriteBehindResult, error) {
	a.calls.Add(1)
	a.items.Add(int64(len(items)))
	if a.sync != nil {
		return a.sync(ctx, items)
	}
	result := &WriteBehindResult{}
	for _, item := range items {
		result.ConfirmedKeys = append(result.ConfirmedKeys, item.Key)
	}
	return result, nil
}
func (a *selectionLoadTarget) OrderingGroup(item *SyncQueueItem[selectionLoadRecord]) (string, error) {
	return fmt.Sprint(item.Item.Group), nil
}
func (a *selectionLoadTarget) HandleFailure(ctx context.Context, f WriteBehindFailure[selectionLoadRecord]) (WriteBehindDecision, error) {
	if a.handle != nil {
		return a.handle(ctx, f)
	}
	return WriteBehindDecision{ID: f.Record.ID, Version: f.Record.Snapshot.UpdatedAt, Fingerprint: f.Record.Fingerprint, Action: WriteBehindPause, Scope: WriteBehindScopeKey, Outcome: WriteBehindStillUnknown}, nil
}
func selectionRecord(key string, group int) *selectionLoadRecord {
	return &selectionLoadRecord{Model: entity.NewModel(), Key: key, Group: group}
}
func newSelectionStore(t *testing.T, path string, target *selectionLoadTarget, overrides ...func(*BadgerDBConfig)) *ReliableWriteStore[selectionLoadRecord] {
	t.Helper()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = false
	cfg.EnableLogger = false
	cfg.SyncBatchSize = 128
	cfg.SyncFlushThreshold = 32
	cfg.SyncMaxCollectDelay = 20 * time.Millisecond
	cfg.SyncBacklogDrainDelay = 0
	cfg.WriteBehindFailure = &WriteBehindFailurePolicy{StoreID: "core-selection-test", MaxAttempts: 3, MaxElapsed: 5 * time.Minute, MinRetryInterval: time.Second, MaxRetryInterval: 10 * time.Second, HandlerMaxAttempts: 8, HandlerTimeout: 2 * time.Second, HandlerWorkers: 2, ScanLimit: 128}
	for _, override := range overrides {
		override(&cfg)
	}
	store, _, err := NewReliableWriteStore[selectionLoadRecord](ServiceIdentity{ServiceName: "core-selection-test"}, ReliableWriteStoreConfig{BasePath: path, Badger: cfg, Batch: BatchCommitConfig{MaxBatch: 128, CollectBacklog: 1, CollectWindow: time.Millisecond, QueueCapacity: 1024}, Admission: WriteAdmissionConfig{MaxConcurrent: 128, AcquireTimeout: 2 * time.Second, SoftPending: 5000, HardPending: 10000, MaxBacklogDuration: 5 * time.Minute, HardDiskBytes: 1 << 30}, CloseTimeout: time.Second})
	require.NoError(t, err)
	require.NoError(t, store.UseWriteBehindWithFailureHandler(target, target))
	require.True(t, store.db.manager.db.Opts().SyncWrites)
	require.True(t, store.db.manager.db.Opts().DetectConflicts)
	t.Cleanup(func() { _ = store.Close(context.Background()); _ = CloseSharedManager(store.config.BasePath) })
	return store
}

// TestDispositionSelectionRanges 固定实际IteratorOptions而非耗时阈值；同库另一个合法prefix不能被选取。
func TestDispositionSelectionRanges(t *testing.T) {
	for _, targetMode := range []string{"absent", "present"} {
		t.Run(targetMode, func(t *testing.T) {
			a := &selectionLoadTarget{}
			store := newSelectionStore(t, t.TempDir(), a)
			db := store.db
			for i := range 6 {
				require.NoError(t, store.Save(context.Background(), selectionRecord(fmt.Sprint(i), i)))
			}
			sibling, err := NewSharedBadgerDB[testFund](store.config.BasePath)
			require.NoError(t, err)
			require.NoError(t, sibling.UseWriteBehindWithFailureHandler(&failureTestTarget{}, &failureTestTarget{}))
			t.Cleanup(func() { _ = sibling.Close() })
			require.NoError(t, sibling.Set(newFund("other", "sibling", 1), 0))
			var wanted string
			if targetMode == "present" {
				selected, _, err := db.selectDispositionRound(1)
				require.NoError(t, err)
				require.Len(t, selected, 1)
				_, _, err = db.commitDispositionResults(selected, nil, errors.New("合成未知结果"))
				require.NoError(t, err)
				r := selected[0]
				wanted = r.ID
				d := WriteBehindDecision{ID: r.ID, Version: r.Snapshot.UpdatedAt, Fingerprint: r.Fingerprint, Action: WriteBehindPause, Scope: WriteBehindScopeTarget, Outcome: WriteBehindStillUnknown}
				require.NoError(t, store.ApplyWriteBehindDecision(context.Background(), d))
			}
			require.NoError(t, db.updateWriteBehindTransaction(func(txn *badger.Txn) error { return db.invalidateDispositionTargetRange(txn) }))
			kinds := map[string]int{}
			db.disposition.selectionObserver = func(kind string, options badger.IteratorOptions, _, _ time.Duration) {
				kinds[kind]++
				assert.False(t, options.PrefetchValues)
				switch kind {
				case "target":
					assert.Equal(t, db.dispositionPrefix()+"target:", string(options.Prefix))
				case "group":
					assert.Equal(t, db.dispositionPrefix()+"g:", string(options.Prefix))
				case "queue":
					assert.True(t, strings.HasPrefix(string(options.Prefix), db.dispositionPrefix()+"q:"))
					assert.Len(t, options.Prefix, len(db.dispositionPrefix()+"q:")+65)
				default:
					t.Errorf("未知迭代种类%q", kind)
				}
			}
			selected, notify, err := db.selectDispositionRound(128)
			require.NoError(t, err)
			require.Empty(t, notify)
			if wanted != "" {
				require.Empty(t, selected, "target暂停不得越过恢复项")
			} else {
				require.Len(t, selected, 6)
			}
			for _, r := range selected {
				require.NotEqual(t, "other", r.Snapshot.Item.Key)
				require.Equal(t, 1, r.Attempts)
				require.NotEmpty(t, r.BatchID)
			}
			require.Positive(t, kinds["target"])
			require.Positive(t, kinds["group"])
			require.Positive(t, kinds["queue"])
			require.Equal(t, 1, sibling.GetCachedPendingSyncCount())
		})
	}
}

// TestDispositionSelectionCursorAndBarriers 覆盖中间/末尾/回绕、空范围、未知和暂停前序，不更改预算。
func TestDispositionSelectionCursorAndBarriers(t *testing.T) {
	store := newSelectionStore(t, t.TempDir(), &selectionLoadTarget{})
	db := store.db
	for i := range 5 {
		require.NoError(t, store.Save(context.Background(), selectionRecord(fmt.Sprint(i), i)))
	}
	selected, _, err := db.selectDispositionRound(1)
	require.NoError(t, err)
	require.Len(t, selected, 1)
	old := selected[0]
	_, _, err = db.commitDispositionResults(selected, nil, errors.New("合成未知"))
	require.NoError(t, err)
	require.NoError(t, store.Save(context.Background(), selectionRecord("later", old.Snapshot.Item.Group)))
	d := WriteBehindDecision{ID: old.ID, Version: old.Snapshot.UpdatedAt, Fingerprint: old.Fingerprint, Action: WriteBehindPause, Scope: WriteBehindScopeKey, Outcome: WriteBehindStillUnknown}
	require.NoError(t, store.ApplyWriteBehindDecision(context.Background(), d))
	groups := []string{}
	require.NoError(t, db.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		opts.Prefix = []byte(db.dispositionPrefix() + "g:")
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			groups = append(groups, string(it.Item().Key())[len(opts.Prefix):])
		}
		return nil
	}))
	sort.Strings(groups)
	require.Len(t, groups, 5)
	for _, cursor := range []string{groups[2], groups[4], strings.Repeat("f", 64), ""} {
		require.NoError(t, db.updateWriteBehindTransaction(func(txn *badger.Txn) error { return dispositionWrite(txn, db.dispositionPrefix()+"cursor", cursor) }))
		next, notify, err := db.selectDispositionRound(128)
		require.NoError(t, err)
		require.Empty(t, notify)
		for _, r := range next {
			require.NotEqual(t, old.Group, r.Group)
			require.NotEqual(t, "later", r.Snapshot.Item.Key)
			require.Equal(t, 1, r.Attempts)
		}
		if len(next) > 0 {
			success := map[string]bool{}
			for _, r := range next {
				success[r.Snapshot.Key] = true
			}
			n, _, e := db.commitDispositionResults(next, success, nil)
			require.NoError(t, e)
			require.Equal(t, len(next), n)
		}
	}
	require.NoError(t, db.manager.db.View(func(txn *badger.Txn) error {
		var r WriteBehindFailureRecord[selectionLoadRecord]
		if e := dispositionRead(txn, db.dispositionPrefix()+"r:"+old.ID, &r); e != nil {
			return e
		}
		require.Equal(t, WriteBehindPaused, r.State)
		require.Equal(t, 1, r.Attempts)
		return nil
	}))
	require.Equal(t, 2, db.GetCachedPendingSyncCount())
}

// TestDispositionSelectionSharesFIFOWithSave 用受控屏障证明真实元数据事务和前台共享提交器；不改FIFO来造提速。
func TestDispositionSelectionSharesFIFOWithSave(t *testing.T) {
	store := newSelectionStore(t, t.TempDir(), &selectionLoadTarget{})
	db := store.db
	require.NoError(t, store.Save(context.Background(), selectionRecord("seed", 1)))
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	db.disposition.selectionObserver = func(kind string, _ badger.IteratorOptions, _, _ time.Duration) {
		if kind == "target" || kind == "target_cached" {
			once.Do(func() { close(entered); <-release })
		}
	}
	var released sync.Once
	t.Cleanup(func() { released.Do(func() { close(release) }) })
	bg := make(chan error, 1)
	go func() { _, _, err := db.selectDispositionRound(128); bg <- err }()
	<-entered
	done := make(chan error, 1)
	go func() { done <- store.Save(context.Background(), selectionRecord("new", 2)) }()
	require.Eventually(t, func() bool { return len(store.batcher.requests) > 0 }, time.Second, time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("选批事务未退出，前台却已返回:%v", err)
	default:
	}
	released.Do(func() { close(release) })
	require.NoError(t, <-bg)
	require.NoError(t, <-done)
	require.Equal(t, 2, store.Metrics().Pending)
}

// TestDispositionSelectionCommitConflict 选批Commit真实冲突时，attempt/批次/cursor写集不能部分生效。
func TestDispositionSelectionCommitConflict(t *testing.T) {
	store := newSelectionStore(t, t.TempDir(), &selectionLoadTarget{})
	db := store.db
	for i := range 8 {
		require.NoError(t, store.Save(context.Background(), selectionRecord(fmt.Sprint(i), i)))
	}
	var once sync.Once
	var injected error
	db.disposition.selectionObserver = func(kind string, _ badger.IteratorOptions, _, _ time.Duration) {
		if kind == "queue" {
			once.Do(func() {
				injected = db.manager.db.Update(func(txn *badger.Txn) error {
					return dispositionWrite(txn, db.dispositionPrefix()+"cursor", strings.Repeat("f", 64))
				})
			})
		}
	}
	selected, _, err := db.selectDispositionRound(8)
	require.NoError(t, injected)
	require.ErrorIs(t, err, badger.ErrConflict)
	require.Len(t, selected, 8)
	require.Equal(t, 8, db.GetCachedPendingSyncCount())
	require.NoError(t, db.manager.db.View(func(txn *badger.Txn) error {
		for _, r := range selected {
			var current WriteBehindFailureRecord[selectionLoadRecord]
			if e := dispositionRead(txn, db.dispositionPrefix()+"r:"+r.ID, &current); e != nil {
				return e
			}
			assert.Equal(t, WriteBehindReady, current.State)
			assert.Zero(t, current.Attempts)
			assert.Empty(t, current.BatchID)
		}
		return nil
	}))
	db.disposition.selectionObserver = nil
	next, _, err := db.selectDispositionRound(8)
	require.NoError(t, err)
	require.Empty(t, next, "末尾cursor只在本轮回绕，不能提前改变顺序")
	next, _, err = db.selectDispositionRound(8)
	require.NoError(t, err)
	require.Len(t, next, 8)
	for _, r := range next {
		require.Equal(t, 1, r.Attempts)
	}
}

// TestDispositionSelectionTooBigIsAtomic 真实过大Save按既有二分提交，选批过大仍整体失败，不能留下部分attempt。
func TestDispositionSelectionTooBigIsAtomic(t *testing.T) {
	store := newSelectionStore(t, t.TempDir(), &selectionLoadTarget{}, func(cfg *BadgerDBConfig) { cfg.MemTableSize = 1 << 20 })
	db := store.db
	items := []*selectionLoadRecord{}
	for i := range 8 {
		r := selectionRecord(fmt.Sprint(i), i)
		r.Payload = make([]byte, 20000)
		items = append(items, r)
	}
	n, err := store.SaveBatch(context.Background(), items)
	require.NoError(t, err)
	require.Equal(t, 8, n.Committed)
	version := db.manager.db.MaxVersion()
	_, _, err = db.selectDispositionRound(8)
	require.ErrorIs(t, err, badger.ErrTxnTooBig)
	require.Equal(t, version, db.manager.db.MaxVersion())
	require.NoError(t, db.manager.db.View(func(txn *badger.Txn) error {
		for _, item := range items {
			var id string
			if e := dispositionRead(txn, db.dispositionPrefix()+"key:"+dispositionHash(db.prefix+item.Key), &id); e != nil {
				return e
			}
			var r WriteBehindFailureRecord[selectionLoadRecord]
			if e := dispositionRead(txn, db.dispositionPrefix()+"r:"+id, &r); e != nil {
				return e
			}
			assert.Equal(t, WriteBehindReady, r.State)
			assert.Zero(t, r.Attempts)
			assert.Empty(t, r.BatchID)
		}
		return nil
	}))
	require.Equal(t, 8, db.GetCachedPendingSyncCount())
	selected, _, err := db.selectDispositionRound(1)
	require.NoError(t, err)
	require.Len(t, selected, 1)
	require.Equal(t, 1, selected[0].Attempts)
}

// TestDispositionSelectionCallbacksOutsideCommitter 远端target/应用失败回调被屏障阻塞时，独立前台Save仍能完成。
func TestDispositionSelectionCallbacksOutsideCommitter(t *testing.T) {
	for _, phase := range []string{"target", "handler"} {
		t.Run(phase, func(t *testing.T) {
			enter, release := make(chan struct{}), make(chan struct{})
			var released sync.Once
			a := &selectionLoadTarget{}
			a.sync = func(_ context.Context, items []*SyncQueueItem[selectionLoadRecord]) (*WriteBehindResult, error) {
				if phase == "handler" {
					return nil, errors.New("合成未知结果")
				}
				close(enter)
				<-release
				return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
			}
			a.handle = func(_ context.Context, f WriteBehindFailure[selectionLoadRecord]) (WriteBehindDecision, error) {
				close(enter)
				<-release
				return WriteBehindDecision{ID: f.Record.ID, Version: f.Record.Snapshot.UpdatedAt, Fingerprint: f.Record.Fingerprint, Action: WriteBehindPause, Scope: WriteBehindScopeKey, Outcome: WriteBehindStillUnknown}, nil
			}
			store := newSelectionStore(t, t.TempDir(), a)
			t.Cleanup(func() { released.Do(func() { close(release) }) })
			require.NoError(t, store.Save(context.Background(), selectionRecord("first", 1)))
			bg := make(chan error, 1)
			go func() { _, err := store.ForceSyncBatch(context.Background(), 1); bg <- err }()
			select {
			case <-enter:
			case <-time.After(3 * time.Second):
				t.Fatal("回调未进入受控屏障")
			}
			front := make(chan error, 1)
			go func() { front <- store.Save(context.Background(), selectionRecord("independent", 2)) }()
			select {
			case err := <-front:
				require.NoError(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("外部回调不应占用本地提交器")
			}
			released.Do(func() { close(release) })
			if phase == "target" {
				require.NoError(t, <-bg)
			} else {
				require.Error(t, <-bg)
			}
		})
	}
}

// TestDispositionSelectionUnknownPrevious 未知原版本阻挡同组和跨组新版本，健康组继续；旧确认不能确认当前版本。
func TestDispositionSelectionUnknownPrevious(t *testing.T) {
	store := newSelectionStore(t, t.TempDir(), &selectionLoadTarget{})
	db := store.db
	require.NoError(t, store.Save(context.Background(), selectionRecord("versioned", 0)))
	first, _, err := db.selectDispositionRound(1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	old := first[0]
	_, _, err = db.commitDispositionResults(first, nil, errors.New("合成未知原版本"))
	require.NoError(t, err)
	for _, r := range []*selectionLoadRecord{selectionRecord("versioned", 1), selectionRecord("later", 0), selectionRecord("healthy", 2)} {
		require.NoError(t, store.Save(context.Background(), r))
	}
	next, notify, err := db.selectDispositionRound(128)
	require.NoError(t, err)
	require.Len(t, next, 1)
	require.Equal(t, "healthy", next[0].Snapshot.Item.Key)
	require.Len(t, notify, 1)
	require.Equal(t, old.ID, notify[0].ID)
	require.Equal(t, 1, notify[0].Attempts)
	n, _, err := db.commitDispositionResults(next, map[string]bool{next[0].Snapshot.Key: true}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.NoError(t, store.ApplyWriteBehindDecision(context.Background(), WriteBehindDecision{ID: old.ID, Version: old.Snapshot.UpdatedAt, Fingerprint: old.Fingerprint, Action: WriteBehindConfirm, Scope: WriteBehindScopeKey, Outcome: WriteBehindCommitted}))
	w, err := db.getWrapper(db.prefix + "versioned")
	require.NoError(t, err)
	require.False(t, w.IsSynced)
	require.Equal(t, 1, w.Item.Group)
	require.Equal(t, 2, db.GetCachedPendingSyncCount())
	next, _, err = db.selectDispositionRound(128)
	require.NoError(t, err)
	require.Len(t, next, 2)
	for _, r := range next {
		require.Equal(t, 1, r.Attempts)
	}
}

// TestDispositionSelectionEmptyRanges 没有组/target/queue时仍有界返回并保留空cursor。
func TestDispositionSelectionEmptyRanges(t *testing.T) {
	store := newSelectionStore(t, t.TempDir(), &selectionLoadTarget{})
	db := store.db
	selected, notify, err := db.selectDispositionRound(128)
	require.NoError(t, err)
	require.Empty(t, selected)
	require.Empty(t, notify)
	require.NoError(t, db.manager.db.View(func(txn *badger.Txn) error {
		var cursor string
		e := dispositionRead(txn, db.dispositionPrefix()+"cursor", &cursor)
		assert.Empty(t, cursor)
		return e
	}))
	require.Zero(t, db.GetCachedPendingSyncCount())
}

// TestDispositionSelectionReusesUnchangedTargetRange 即使Prefix正确，同一未变化的空target范围也不应逐轮重扫历史。
func TestDispositionSelectionReusesUnchangedTargetRange(t *testing.T) {
	store := newSelectionStore(t, t.TempDir(), &selectionLoadTarget{})
	db := store.db
	seeks := 0
	db.disposition.storageMu.Lock()
	db.disposition.targetCacheValid = false
	db.disposition.storageMu.Unlock()
	db.disposition.selectionObserver = func(kind string, _ badger.IteratorOptions, _, _ time.Duration) {
		if kind == "target" {
			seeks++
		}
	}
	for range 3 {
		selected, _, err := db.selectDispositionRound(128)
		require.NoError(t, err)
		require.Empty(t, selected)
	}
	require.Equal(t, 1, seeks, "只复用经持久版本核验的target范围，不按ScanLimit反复跳过同一段删除历史")
}

// TestDispositionSelectionTargetMembershipInvalidatesAcrossWrappers 同物理库同prefix的另一正式入口改变target屏障，必须原子失效。
func TestDispositionSelectionTargetMembershipInvalidatesAcrossWrappers(t *testing.T) {
	store := newSelectionStore(t, t.TempDir(), &selectionLoadTarget{})
	db := store.db
	other, e := NewSharedBadgerDB[selectionLoadRecord](store.config.BasePath)
	require.NoError(t, e)
	a := &selectionLoadTarget{}
	require.NoError(t, other.UseWriteBehindWithFailureHandler(a, a))
	t.Cleanup(func() { _ = other.Close() })
	require.NoError(t, store.Save(context.Background(), selectionRecord("blocked", 1)))
	selected, _, e := other.selectDispositionRound(1)
	require.NoError(t, e)
	require.Len(t, selected, 1)
	old := selected[0]
	_, _, e = other.commitDispositionResults(selected, nil, errors.New("合成未知"))
	require.NoError(t, e)
	// 先在另一个wrapper已初始化后建立“无全局target”的快照，排除bootstrap写mode偶然失效。
	_, _, e = db.selectDispositionRound(128)
	require.NoError(t, e)
	var beforeVersion uint64
	var fingerprint []byte
	require.NoError(t, db.manager.db.View(func(txn *badger.Txn) error {
		item, e := txn.Get([]byte(db.dispositionPrefix() + "mode"))
		if e != nil {
			return e
		}
		beforeVersion = item.Version()
		fingerprint, e = item.ValueCopy(nil)
		return e
	}))
	d := WriteBehindDecision{ID: old.ID, Version: old.Snapshot.UpdatedAt, Fingerprint: old.Fingerprint, Action: WriteBehindPause, Scope: WriteBehindScopeTarget, Outcome: WriteBehindStillUnknown}
	require.NoError(t, other.ApplyWriteBehindDecision(context.Background(), d))
	require.NoError(t, db.manager.db.View(func(txn *badger.Txn) error {
		item, e := txn.Get([]byte(db.dispositionPrefix() + "mode"))
		if e != nil {
			return e
		}
		assert.Greater(t, item.Version(), beforeVersion, "target成员变更与既有mode版本必须同事务提交")
		v, e := item.ValueCopy(nil)
		assert.Equal(t, fingerprint, v, "policy字节/指纹不改变")
		return e
	}))
	require.NoError(t, store.Save(context.Background(), selectionRecord("healthy", 2)))
	selected, _, e = db.selectDispositionRound(128)
	require.NoError(t, e)
	require.Empty(t, selected, "新target暂停屏障不能被旧的空快照绕过")
	d.Action = WriteBehindConfirm
	d.Outcome = WriteBehindCommitted
	d.Scope = WriteBehindScopeKey
	require.NoError(t, other.ApplyWriteBehindDecision(context.Background(), d))
	selected, _, e = db.selectDispositionRound(128)
	require.NoError(t, e)
	require.Len(t, selected, 1)
	require.Equal(t, "healthy", selected[0].Snapshot.Item.Key)
}

// TestDispositionSelectionTargetMutationRollback 失败事务内的target变更不能污染后续范围快照，也不能留下mode新版本。
func TestDispositionSelectionTargetMutationRollback(t *testing.T) {
	store := newSelectionStore(t, t.TempDir(), &selectionLoadTarget{})
	db := store.db
	require.NoError(t, store.Save(context.Background(), selectionRecord("one", 1)))
	selected, _, e := db.selectDispositionRound(1)
	require.NoError(t, e)
	require.Len(t, selected, 1)
	_, _, e = db.commitDispositionResults(selected, nil, errors.New("合成未知"))
	require.NoError(t, e)
	sentinel := errors.New("合成事务回滚")
	version := db.manager.db.MaxVersion()
	r := selected[0]
	r.Scope = WriteBehindScopeTarget
	r.State = WriteBehindPaused
	e = db.updateWriteBehindTransaction(func(txn *badger.Txn) error {
		if e := db.saveDisposition(txn, &r); e != nil {
			return e
		}
		return sentinel
	})
	require.ErrorIs(t, e, sentinel)
	require.Equal(t, version, db.manager.db.MaxVersion())
	require.NoError(t, store.Save(context.Background(), selectionRecord("healthy", 2)))
	selected, notify, e := db.selectDispositionRound(128)
	require.NoError(t, e)
	require.Len(t, selected, 1)
	require.Len(t, notify, 1)
	require.Equal(t, "healthy", selected[0].Snapshot.Item.Key)
}

// TestDispositionSelectionTargetVersionFencesConcurrentWrapper 用真实Commit冲突证明缓存命中也读入target代际，不漏并发全局屏障。
func TestDispositionSelectionTargetVersionFencesConcurrentWrapper(t *testing.T) {
	store := newSelectionStore(t, t.TempDir(), &selectionLoadTarget{})
	db := store.db
	other, e := NewSharedBadgerDB[selectionLoadRecord](store.config.BasePath)
	require.NoError(t, e)
	a := &selectionLoadTarget{}
	require.NoError(t, other.UseWriteBehindWithFailureHandler(a, a))
	t.Cleanup(func() { _ = other.Close() })
	require.NoError(t, other.Set(selectionRecord("blocked", 1), 0))
	selected, _, e := other.selectDispositionRound(1)
	require.NoError(t, e)
	require.Len(t, selected, 1)
	old := selected[0]
	_, _, e = other.commitDispositionResults(selected, nil, errors.New("合成未知"))
	require.NoError(t, e)
	require.NoError(t, store.Save(context.Background(), selectionRecord("healthy", 2)))
	require.NoError(t, db.updateWriteBehindTransaction(func(txn *badger.Txn) error { _, e := db.dispositionTargetID(txn); return e }))
	enter, release := make(chan struct{}), make(chan struct{})
	var once, released sync.Once
	db.disposition.selectionObserver = func(kind string, _ badger.IteratorOptions, _, _ time.Duration) {
		if kind == "target_cached" {
			once.Do(func() { close(enter); <-release })
		}
	}
	t.Cleanup(func() { released.Do(func() { close(release) }) })
	done := make(chan error, 1)
	go func() { _, _, e := db.selectDispositionRound(128); done <- e }()
	<-enter
	require.NoError(t, other.ApplyWriteBehindDecision(context.Background(), WriteBehindDecision{ID: old.ID, Version: old.Snapshot.UpdatedAt, Fingerprint: old.Fingerprint, Action: WriteBehindPause, Scope: WriteBehindScopeTarget, Outcome: WriteBehindStillUnknown}))
	released.Do(func() { close(release) })
	require.ErrorIs(t, <-done, badger.ErrConflict)
	db.disposition.selectionObserver = nil
	selected, _, e = db.selectDispositionRound(128)
	require.NoError(t, e)
	require.Empty(t, selected)
	pending, e := db.GetPendingSyncCount()
	require.NoError(t, e)
	require.Equal(t, 2, pending)
}

// TestDispositionSelectionWritesSnapshotOnce 八个原件的最终写集能装入真实事务预算，不应因同快照重复Set而失败。
func TestDispositionSelectionWritesSnapshotOnce(t *testing.T) {
	store := newSelectionStore(t, t.TempDir(), &selectionLoadTarget{}, func(cfg *BadgerDBConfig) { cfg.MemTableSize = 1 << 20 })
	db := store.db
	items := []*selectionLoadRecord{}
	for i := range 8 {
		r := selectionRecord(fmt.Sprint(i), i)
		r.Payload = make([]byte, 10000)
		items = append(items, r)
	}
	n, e := store.SaveBatch(context.Background(), items)
	require.NoError(t, e)
	require.Equal(t, 8, n.Committed)
	before := db.manager.db.MaxVersion()
	selected, _, e := db.selectDispositionRound(8)
	require.NoError(t, e, "实际最终写集可装入预算，不得先写一次无BatchID的同快照")
	require.Len(t, selected, 8)
	require.Equal(t, before+1, db.manager.db.MaxVersion())
	batch := selected[0].BatchID
	require.NotEmpty(t, batch)
	require.NoError(t, db.manager.db.View(func(txn *badger.Txn) error {
		for _, r := range selected {
			var saved WriteBehindFailureRecord[selectionLoadRecord]
			if e := dispositionRead(txn, db.dispositionPrefix()+"r:"+r.ID, &saved); e != nil {
				return e
			}
			assert.Equal(t, WriteBehindInFlight, saved.State)
			assert.Equal(t, 1, saved.Attempts)
			assert.Equal(t, batch, saved.BatchID)
		}
		return nil
	}))
	require.Equal(t, 8, db.GetCachedPendingSyncCount())
	require.Zero(t, store.Metrics().Sync.SyncedItems)
}

// TestDispositionSelectionPrimesTargetBeforeBinding 第一轮后台不能把原卷target空范围的冷扫描转嫁给新进入的前台。
func TestDispositionSelectionPrimesTargetBeforeBinding(t *testing.T) {
	store := newSelectionStore(t, t.TempDir(), &selectionLoadTarget{})
	db := store.db
	seeks := 0
	db.disposition.selectionObserver = func(kind string, _ badger.IteratorOptions, _, _ time.Duration) {
		if kind == "target" {
			seeks++
		}
	}
	selected, _, e := db.selectDispositionRound(128)
	require.NoError(t, e)
	require.Empty(t, selected)
	require.Zero(t, seeks, "绑定返回前应已建立经事务版本核验的target范围；不靠首笔Save触发冷恢复")
}

// TestDispositionSelectionUnchangedCursorDoesNotWrite 空轮/全暂停且cursor未变时，不应写相同cursor制造可靠提交。
func TestDispositionSelectionUnchangedCursorDoesNotWrite(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(fmt.Sprint(paused), func(t *testing.T) {
			store := newSelectionStore(t, t.TempDir(), &selectionLoadTarget{})
			db := store.db
			if paused {
				require.NoError(t, store.Save(context.Background(), selectionRecord("paused", 1)))
				selected, _, e := db.selectDispositionRound(1)
				require.NoError(t, e)
				require.Len(t, selected, 1)
				r := selected[0]
				_, _, e = db.commitDispositionResults(selected, nil, errors.New("合成未知"))
				require.NoError(t, e)
				require.NoError(t, store.ApplyWriteBehindDecision(context.Background(), WriteBehindDecision{ID: r.ID, Version: r.Snapshot.UpdatedAt, Fingerprint: r.Fingerprint, Action: WriteBehindPause, Scope: WriteBehindScopeKey, Outcome: WriteBehindStillUnknown}))
			}
			_, _, e := db.selectDispositionRound(128)
			require.NoError(t, e) // 保留首次cursor建立/回绕的原布局。
			before := db.manager.db.MaxVersion()
			for range 3 {
				selected, _, e := db.selectDispositionRound(128)
				require.NoError(t, e)
				require.Empty(t, selected)
			}
			require.Equal(t, before, db.manager.db.MaxVersion(), "实际Badger提交版本不能因重复写相同cursor增长")
			if paused {
				require.Equal(t, 1, db.GetCachedPendingSyncCount())
			}
		})
	}
}

// TestDispositionSelectionTargetReopensInNewProcess 原卷target屏障由新PID重建缓存；不靠新Save唤醒。
func TestDispositionSelectionTargetReopensInNewProcess(t *testing.T) {
	path := t.TempDir()
	store := newSelectionStore(t, path, &selectionLoadTarget{})
	for i := range 3 {
		require.NoError(t, store.Save(context.Background(), selectionRecord(fmt.Sprint(i), i)))
	}
	selected, _, err := store.db.selectDispositionRound(1)
	require.NoError(t, err)
	require.Len(t, selected, 1)
	_, _, err = store.db.commitDispositionResults(selected, nil, errors.New("合成未知结果"))
	require.NoError(t, err)
	r := selected[0]
	require.NoError(t, store.ApplyWriteBehindDecision(context.Background(), WriteBehindDecision{ID: r.ID, Version: r.Snapshot.UpdatedAt, Fingerprint: r.Fingerprint, Action: WriteBehindPause, Scope: WriteBehindScopeTarget, Outcome: WriteBehindStillUnknown}))
	var pending *PendingSyncError
	require.ErrorAs(t, store.Close(context.Background()), &pending)
	require.NoError(t, CloseSharedManager(store.config.BasePath))
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(exe, "-test.run=^TestDispositionSelectionReopenProcess$", "-test.v", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "CORE_SELECTION_REOPEN_PATH="+path, "CORE_SELECTION_REOPEN_MODE=target")
	output, err := cmd.CombinedOutput()
	require.NoError(t, os.WriteFile(filepath.Join(path, "selection-reopen.log"), output, 0600))
	require.NoError(t, err, string(output))
	t.Logf("父PID=%d，新PID=%d，target原件/预算/组屏障及显式提交后恢复已核对: %s", os.Getpid(), cmd.ProcessState.Pid(), output)
}
