// 本文件验证有界确认、本地回收和并发版本/失败状态/真实存储重启之间的边界。
package nosql

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

func cleanupTestAwaitState(t *testing.T, store *ReliableWriteStore[cleanupTestRecord], state WriteBehindFailureState) WriteBehindFailureRecord[cleanupTestRecord] {
	t.Helper()
	var found WriteBehindFailureRecord[cleanupTestRecord]
	require.Eventually(t, func() bool {
		records, _, err := store.ListWriteBehindFailures("", 16)
		if err != nil {
			return false
		}
		for _, r := range records {
			if r.State == state {
				found = r
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond)
	return found
}

func cleanupTestConfirm(t *testing.T, store *ReliableWriteStore[cleanupTestRecord], r WriteBehindFailureRecord[cleanupTestRecord]) {
	t.Helper()
	d := cleanupTestDecision(r, WriteBehindConfirm)
	d.Outcome = WriteBehindCommitted
	require.NoError(t, store.ApplyWriteBehindDecision(context.Background(), d))
}

func cleanupTestPending(t *testing.T, store *ReliableWriteStore[cleanupTestRecord], count int) {
	t.Helper()
	require.Equal(t, count, store.Metrics().Pending)
	actual, err := store.db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, count, actual)
}

func cleanupTestDeleted(t *testing.T, store *ReliableWriteStore[cleanupTestRecord], key string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := store.GetLocal(context.Background(), key)
		return errors.Is(err, badger.ErrKeyNotFound)
	}, time.Second, time.Millisecond)
}

// TestBoundedCleanupFalseThenTrue 验证两条确认路径都只在新版本 true 经本地确认后回收，重复确认不扣数或重复调度。
func TestBoundedCleanupFalseThenTrue(t *testing.T) {
	for _, application := range []bool{false, true} {
		t.Run(fmt.Sprintf("application=%t", application), func(t *testing.T) {
			target := &cleanupTestTarget{}
			if application {
				target.sync = func(context.Context, []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
					return nil, errors.New("测试需要应用核实")
				}
			}
			store := openCleanupTestStore(t, t.TempDir(), false, true, target)
			for revision := 1; revision <= 2; revision++ {
				probe := &cleanupTestProbe{}
				value := newCleanupTestRecord(t, "same-key", revision, revision == 2, probe)
				require.NoError(t, store.Save(context.Background(), value))
				cleanupTestPending(t, store, 1)
				require.Zero(t, probe.calls.Load(), "尚未确认的新版本不能提前回收")
				result, err := store.ForceSyncBatch(context.Background(), 16)
				if application {
					require.Error(t, err)
					r := cleanupTestAwaitState(t, store, WriteBehindPaused)
					cleanupTestConfirm(t, store, r)
					cleanupTestConfirm(t, store, r)
				} else {
					require.NoError(t, err)
					require.Equal(t, 1, result.Confirmed)
				}
				require.Eventually(t, func() bool { return probe.calls.Load() == 1 }, time.Second, time.Millisecond)
				cleanupTestPending(t, store, 0)
				if revision == 1 {
					local, err := store.GetLocal(context.Background(), value.Key)
					require.NoError(t, err)
					require.Equal(t, revision, local.Revision)
				} else {
					cleanupTestDeleted(t, store, value.Key)
				}
			}
			records, _, err := store.ListWriteBehindFailures("", 16)
			require.NoError(t, err)
			require.Len(t, records, 2, "本地模型回收不得删除生命周期历史")
			require.Equal(t, uint64(2), store.Metrics().Sync.SyncedItems)
		})
	}
}

// TestBoundedCleanupOldTargetCannotDeleteNewVersion 验证 target 在途时保存新值，旧成功回执既不确认也不调度当前记录。
func TestBoundedCleanupOldTargetCannotDeleteNewVersion(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		if items[0].Item.Revision == 1 {
			once.Do(func() { close(entered) })
			<-release
		}
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}}
	oldProbe, newProbe := &cleanupTestProbe{}, &cleanupTestProbe{}
	old := newCleanupTestRecord(t, "same", 1, true, oldProbe)
	newValue := newCleanupTestRecord(t, "same", 2, true, newProbe)
	store := openCleanupTestStore(t, t.TempDir(), false, true, target)
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	require.NoError(t, store.Save(context.Background(), old))
	done := make(chan ForceSyncResult, 1)
	errorsCh := make(chan error, 1)
	go func() { r, e := store.ForceSyncBatch(context.Background(), 16); done <- r; errorsCh <- e }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("target 没有进入受控屏障")
	}
	require.NoError(t, store.Save(context.Background(), newValue))
	unblock.Do(func() { close(release) })
	require.Equal(t, 0, (<-done).Confirmed)
	require.NoError(t, <-errorsCh)
	cleanupTestPending(t, store, 1)
	require.Zero(t, oldProbe.calls.Load())
	require.Zero(t, newProbe.calls.Load())
	local, err := store.GetLocal(context.Background(), old.Key)
	require.NoError(t, err)
	require.Equal(t, 2, local.Revision)
	_, err = store.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, err)
	cleanupTestDeleted(t, store, old.Key)
	require.Zero(t, oldProbe.calls.Load())
	require.Equal(t, int32(1), newProbe.calls.Load())
}

// TestBoundedCleanupOldDecisionAndFingerprint 验证旧应用回执或错误指纹不得回收新版本。
func TestBoundedCleanupOldDecisionAndFingerprint(t *testing.T) {
	probe := &cleanupTestProbe{}
	old := newCleanupTestRecord(t, "decision", 1, true, probe)
	target := &cleanupTestTarget{sync: func(context.Context, []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		return nil, errors.New("测试未知结果")
	}}
	store := openCleanupTestStore(t, t.TempDir(), false, true, target)
	require.NoError(t, store.Save(context.Background(), old))
	_, err := store.ForceSyncBatch(context.Background(), 16)
	require.Error(t, err)
	r := cleanupTestAwaitState(t, store, WriteBehindPaused)
	d := cleanupTestDecision(r, WriteBehindConfirm)
	d.Outcome, d.Fingerprint = WriteBehindCommitted, "wrong-fingerprint"
	require.ErrorIs(t, store.ApplyWriteBehindDecision(context.Background(), d), ErrWriteBehindDisposition)
	cleanupTestPending(t, store, 1)
	require.Zero(t, probe.calls.Load())
	newProbe := &cleanupTestProbe{}
	newValue := newCleanupTestRecord(t, old.Key, 2, true, newProbe)
	require.NoError(t, store.Save(context.Background(), newValue))
	cleanupTestConfirm(t, store, r)
	cleanupTestConfirm(t, store, r)
	cleanupTestPending(t, store, 1)
	local, err := store.GetLocal(context.Background(), old.Key)
	require.NoError(t, err)
	require.Equal(t, 2, local.Revision)
	require.Zero(t, probe.calls.Load())
	require.Zero(t, newProbe.calls.Load())
	require.Zero(t, store.Metrics().Sync.SyncedItems)
}

// TestBoundedCleanupCurrentFingerprintConflict 验证版本时间相同但持久内容冲突时也不能确认或回收。
func TestBoundedCleanupCurrentFingerprintConflict(t *testing.T) {
	probe := &cleanupTestProbe{}
	value := newCleanupTestRecord(t, "fingerprint", 1, true, probe)
	var store *ReliableWriteStore[cleanupTestRecord]
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		err := store.db.updateWriteBehindTransaction(func(txn *badger.Txn) error {
			var current SyncQueueItem[cleanupTestRecord]
			if err := dispositionRead(txn, items[0].Key, &current); err != nil {
				return err
			}
			current.Item.Revision++ // 故障注入：保留时间戳，只改变内容以独立验证指纹。
			return dispositionWrite(txn, current.Key, &current)
		})
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, err
	}}
	store = openCleanupTestStore(t, t.TempDir(), false, true, target)
	require.NoError(t, store.Save(context.Background(), value))
	result, err := store.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, err)
	require.Zero(t, result.Confirmed)
	cleanupTestPending(t, store, 1)
	require.Zero(t, probe.calls.Load())
	wrapper, err := store.db.getWrapper(store.db.generateKey(value))
	require.NoError(t, err)
	require.False(t, wrapper.IsSynced)
	require.Equal(t, 2, wrapper.Item.Revision)
}

// TestBoundedCleanupNewSaveDuringHook 验证清理 hook 已进入后并发保存新版本，删除阶段仍须重新校验版本。
func TestBoundedCleanupNewSaveDuringHook(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	probe := &cleanupTestProbe{hook: func(*cleanupTestRecord) { close(entered); <-release }}
	old := newCleanupTestRecord(t, "hook-race", 1, true, probe)
	newProbe := &cleanupTestProbe{}
	newValue := newCleanupTestRecord(t, old.Key, 2, true, newProbe)
	base := t.TempDir()
	store := openCleanupTestStore(t, base, false, true, &cleanupTestTarget{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	require.NoError(t, store.Save(context.Background(), old))
	_, err := store.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, err)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("清理 hook 未进入")
	}
	require.NoError(t, store.Save(context.Background(), newValue))
	unblock.Do(func() { close(release) })
	// 关闭等待清理线程真正退出，再重开原卷验证旧 hook 没有删除新 pending。
	var pending *PendingSyncError
	require.ErrorAs(t, store.Close(context.Background()), &pending)
	require.NoError(t, CloseSharedManager(store.config.BasePath))
	store = openCleanupTestStore(t, base, false, true, &cleanupTestTarget{})
	cleanupTestPending(t, store, 1)
	local, err := store.GetLocal(context.Background(), old.Key)
	require.NoError(t, err)
	require.Equal(t, 2, local.Revision)
	_, err = store.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, err)
	cleanupTestDeleted(t, store, old.Key)
	require.Equal(t, int32(1), probe.calls.Load())
	require.Equal(t, int32(1), newProbe.calls.Load())
}

// TestBoundedCleanupPartialSuccess 验证成功/失败并存和重复成功 key，仅实际确认项可回收，原错误及失败历史保留。
func TestBoundedCleanupPartialSuccess(t *testing.T) {
	originalErr := errors.New("测试部分失败")
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		result := &WriteBehindResult{}
		for _, item := range items {
			if item.Item.Key == "healthy" {
				result.ConfirmedKeys = append(result.ConfirmedKeys, item.Key, item.Key)
			}
		}
		return result, originalErr
	}}
	probes := []*cleanupTestProbe{{}, {}, {}}
	store := openCleanupTestStore(t, t.TempDir(), false, true, target)
	for i, key := range []string{"healthy", "failed-a", "failed-b"} {
		require.NoError(t, store.Save(context.Background(), newCleanupTestRecord(t, key, 1, true, probes[i])))
	}
	result, err := store.ForceSyncBatch(context.Background(), 16)
	require.ErrorIs(t, err, originalErr)
	require.Equal(t, 1, result.Confirmed)
	cleanupTestDeleted(t, store, "healthy")
	cleanupTestPending(t, store, 2)
	require.Equal(t, int32(1), probes[0].calls.Load())
	for i, key := range []string{"failed-a", "failed-b"} {
		_, err := store.GetLocal(context.Background(), key)
		require.NoError(t, err)
		require.Zero(t, probes[i+1].calls.Load())
	}
	require.Equal(t, uint64(1), store.Metrics().Sync.SyncedItems)
	require.Equal(t, uint64(1), store.Metrics().Sync.Failures)
	records, _, err := store.ListWriteBehindFailures("", 16)
	require.NoError(t, err)
	require.Len(t, records, 3)
}

// TestBoundedCleanupLocalConfirmationFailure 验证 target/应用确认遇到真实本地库故障时，不清理、不扣 pending，原卷可恢复。
func TestBoundedCleanupLocalConfirmationFailure(t *testing.T) {
	for _, application := range []bool{false, true} {
		t.Run(fmt.Sprintf("application=%t", application), func(t *testing.T) {
			probe := &cleanupTestProbe{}
			value := newCleanupTestRecord(t, "local-failure", 1, true, probe)
			base := t.TempDir()
			var store *ReliableWriteStore[cleanupTestRecord]
			target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
				if application {
					return nil, errors.New("测试应用待确认")
				}
				// 故障在真实 target 返回之后、本地确认开始之前生效，选批已经成功提交。
				if err := store.db.manager.Close(); err != nil {
					return nil, err
				}
				return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
			}}
			store = openCleanupTestStore(t, base, false, true, target)
			require.NoError(t, store.Save(context.Background(), value))
			result, err := store.ForceSyncBatch(context.Background(), 16)
			if application {
				require.Error(t, err)
				r := cleanupTestAwaitState(t, store, WriteBehindPaused)
				require.NoError(t, store.db.manager.Close())
				d := cleanupTestDecision(r, WriteBehindConfirm)
				d.Outcome = WriteBehindCommitted
				err = store.ApplyWriteBehindDecision(context.Background(), d)
			}
			require.ErrorIs(t, err, badger.ErrDBClosed)
			require.Zero(t, result.Confirmed)
			require.Equal(t, 1, store.Metrics().Pending)
			require.Zero(t, probe.calls.Load())
			require.Error(t, store.Close(context.Background()))
			_ = CloseSharedManager(store.config.BasePath)
			reopened := openCleanupTestStore(t, base, false, true, &cleanupTestTarget{})
			cleanupTestPending(t, reopened, 1)
			wrapper, err := reopened.db.getWrapper(reopened.db.generateKey(value))
			require.NoError(t, err)
			require.False(t, wrapper.IsSynced)
			require.Zero(t, probe.calls.Load())
		})
	}
}

// TestBoundedCleanupAbortedConfirmationTransaction 使用真实 Badger 提交冲突，验证 tentative 确认不能在事务内调度清理。
func TestBoundedCleanupAbortedConfirmationTransaction(t *testing.T) {
	probe := &cleanupTestProbe{}
	value := newCleanupTestRecord(t, "abort", 1, true, probe)
	store := openCleanupTestStore(t, t.TempDir(), false, true, &cleanupTestTarget{})
	require.NoError(t, store.Save(context.Background(), value))
	selected, _, err := store.db.selectDispositionRound(16)
	require.NoError(t, err)
	require.Len(t, selected, 1)
	txn := store.db.manager.db.NewTransaction(true)
	defer txn.Discard()
	r := selected[0]
	removed, err := store.db.completeDisposition(txn, &r, WriteBehindCompleted)
	require.NoError(t, err)
	require.True(t, removed, "闭包内的 tentative 结果不等于提交成功")
	require.NoError(t, store.db.manager.db.Update(func(other *badger.Txn) error {
		return other.Set([]byte(store.db.syncQueueKey(r.Snapshot.Key)), nil)
	}))
	require.ErrorIs(t, txn.Commit(), badger.ErrConflict)
	cleanupTestPending(t, store, 1)
	wrapper, err := store.db.getWrapper(r.Snapshot.Key)
	require.NoError(t, err)
	require.False(t, wrapper.IsSynced)
	var pending *PendingSyncError
	require.ErrorAs(t, store.Close(context.Background()), &pending)
	require.Zero(t, probe.calls.Load())
	require.Zero(t, store.db.getSyncAfterDeleteQueueLength())
}

// TestBoundedCleanupPauseHandoffAndRelease 验证暂停、真实持久接管及放行都不能当成业务提交成功。
func TestBoundedCleanupPauseHandoffAndRelease(t *testing.T) {
	probe := &cleanupTestProbe{}
	value := newCleanupTestRecord(t, "handoff", 1, true, probe)
	target := &cleanupTestTarget{sync: func(context.Context, []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		return nil, errors.New("测试未知结果")
	}}
	store := openCleanupTestStore(t, t.TempDir(), false, true, target)
	require.NoError(t, store.Save(context.Background(), value))
	_, err := store.ForceSyncBatch(context.Background(), 16)
	require.Error(t, err)
	r := cleanupTestAwaitState(t, store, WriteBehindPaused)
	cleanupTestPending(t, store, 1)
	require.Zero(t, probe.calls.Load())
	app, err := badger.Open(badger.DefaultOptions(t.TempDir()).WithLogger(nil).WithSyncWrites(true))
	require.NoError(t, err)
	defer app.Close()
	require.NoError(t, app.Update(func(txn *badger.Txn) error { return dispositionWrite(txn, r.ID, &r) }))
	d := cleanupTestDecision(r, WriteBehindHandoff)
	d.Receipt = &WriteBehindHandoffReceipt{DispositionID: r.ID, Version: r.Snapshot.UpdatedAt, Fingerprint: r.Fingerprint, ReceiptID: "durable-test-receipt", Owner: "application", DurableReference: "independent-test-store:" + r.ID}
	require.NoError(t, store.ApplyWriteBehindDecision(context.Background(), d))
	cleanupTestPending(t, store, 0)
	d.Action, d.Outcome = WriteBehindRelease, WriteBehindOrderingReleased
	require.NoError(t, store.ApplyWriteBehindDecision(context.Background(), d))
	require.NoError(t, store.Close(context.Background()))
	require.Zero(t, probe.calls.Load())
	wrapper, err := store.db.getWrapper(r.Snapshot.Key)
	require.NoError(t, err)
	require.True(t, wrapper.HandedOff)
	require.False(t, wrapper.IsSynced)
	require.Zero(t, store.Metrics().Sync.SyncedItems)
}

// TestBoundedCleanupSupersededVersion 验证未投递旧版本被替换，不会调用旧 hook 或删掉替换版本。
func TestBoundedCleanupSupersededVersion(t *testing.T) {
	oldProbe, newProbe := &cleanupTestProbe{}, &cleanupTestProbe{}
	old := newCleanupTestRecord(t, "superseded", 1, true, oldProbe)
	current := newCleanupTestRecord(t, old.Key, 2, true, newProbe)
	store := openCleanupTestStore(t, t.TempDir(), false, true, &cleanupTestTarget{})
	require.NoError(t, store.Save(context.Background(), old))
	require.NoError(t, store.Save(context.Background(), current))
	r := cleanupTestAwaitState(t, store, WriteBehindSuperseded)
	d := cleanupTestDecision(r, WriteBehindConfirm)
	d.Outcome = WriteBehindCommitted
	require.ErrorIs(t, store.ApplyWriteBehindDecision(context.Background(), d), ErrWriteBehindDisposition)
	_, err := store.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, err)
	cleanupTestDeleted(t, store, old.Key)
	require.Zero(t, oldProbe.calls.Load())
	require.Equal(t, int32(1), newProbe.calls.Load())
}

// TestBoundedCleanupLateConfirmation 验证过期回调即使迟到声称 Committed 也不能确认或调度清理。
func TestBoundedCleanupLateConfirmation(t *testing.T) {
	expired, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	probe := &cleanupTestProbe{}
	value := newCleanupTestRecord(t, "late", 1, true, probe)
	target := &cleanupTestTarget{sync: func(context.Context, []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		return nil, errors.New("测试未知结果")
	}, handle: func(ctx context.Context, f WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error) {
		<-ctx.Done()
		close(expired)
		<-release
		d := cleanupTestDecision(f.Record, WriteBehindConfirm)
		d.Outcome = WriteBehindCommitted
		return d, nil
	}}
	store := openCleanupTestStore(t, t.TempDir(), false, true, target)
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	require.NoError(t, store.Save(context.Background(), value))
	_, err := store.ForceSyncBatch(context.Background(), 16)
	require.Error(t, err)
	select {
	case <-expired:
	case <-time.After(2 * time.Second):
		t.Fatal("受控回调未到期")
	}
	_, err = store.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, err)
	cleanupTestAwaitState(t, store, WriteBehindPaused)
	unblock.Do(func() { close(release) })
	var pending *PendingSyncError
	require.ErrorAs(t, store.Close(context.Background()), &pending)
	cleanupTestPending(t, store, 1)
	require.Zero(t, probe.calls.Load())
}

// TestWriteBehindCleanupCommitCloseRestart 明确既有清理队列为内存队列：确认提交后、入队前关闭不重发远端或自动补删。
func TestWriteBehindCleanupCommitCloseRestart(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(fmt.Sprintf("bounded=%t", bounded), func(t *testing.T) {
			probe := &cleanupTestProbe{}
			value := newCleanupTestRecord(t, "close-gap", 1, true, probe)
			base := t.TempDir()
			var calls atomic.Int32
			target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
				calls.Add(1)
				return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
			}}
			store := openCleanupTestStore(t, base, false, bounded, target)
			require.NoError(t, store.Save(context.Background(), value))
			store.db.sadQueueMu.Lock() // 受控停在确认已提交、清理尚未入队的窗口，不靠毫秒竞争。
			var unlock sync.Once
			t.Cleanup(func() { unlock.Do(store.db.sadQueueMu.Unlock) })
			done := make(chan error, 1)
			go func() { _, e := store.ForceSyncBatch(context.Background(), 16); done <- e }()
			require.Eventually(t, func() bool { return store.Metrics().Pending == 0 }, time.Second, time.Millisecond)
			require.NoError(t, store.Close(context.Background()))
			unlock.Do(store.db.sadQueueMu.Unlock)
			require.NoError(t, <-done)
			require.Zero(t, probe.calls.Load())
			require.NoError(t, CloseSharedManager(store.config.BasePath))
			reopened := openCleanupTestStore(t, base, false, bounded, target)
			result, err := reopened.ForceSyncBatch(context.Background(), 16)
			require.NoError(t, err)
			require.Zero(t, result.Confirmed)
			cleanupTestPending(t, reopened, 0)
			require.Equal(t, int32(1), calls.Load(), "已确认项不能因清理未完成重发 target")
			local, err := reopened.GetLocal(context.Background(), value.Key)
			require.NoError(t, err)
			require.Equal(t, 1, local.Revision)
			require.Zero(t, probe.calls.Load(), "现有内存清理队列不承诺跨重启恢复")
			newProbe := &cleanupTestProbe{}
			newValue := newCleanupTestRecord(t, value.Key, 2, true, newProbe)
			require.NoError(t, reopened.Save(context.Background(), newValue))
			_, err = reopened.ForceSyncBatch(context.Background(), 16)
			require.NoError(t, err)
			cleanupTestDeleted(t, reopened, value.Key)
			require.Equal(t, int32(1), newProbe.calls.Load())
			require.Zero(t, probe.calls.Load())
		})
	}
}
