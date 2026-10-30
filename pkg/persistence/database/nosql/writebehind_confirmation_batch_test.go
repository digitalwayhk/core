// 本文件以真实Badger提交版本验证有界确认不会每条单独可靠提交，不依赖计时阈值。
package nosql

import (
	"bytes"
	"context"
	"fmt"
	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDispositionConfirmationUsesBoundedTransactions 固定128个独立组的真实提交放大，选批一次加至多四个确认块。
func TestDispositionConfirmationUsesBoundedTransactions(t *testing.T) {
	a := &failureTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		r := &WriteBehindResult{}
		for _, item := range items {
			r.ConfirmedKeys = append(r.ConfirmedKeys, item.Key)
		}
		return r, nil
	}}
	db := openFailureTestDB(t, t.TempDir(), false, a)
	db.manager.config.SyncBatchSize = 128
	db.disposition.policy.ScanLimit = 128
	for i := range 128 {
		require.NoError(t, db.Set(newFund(fmt.Sprint(i), fmt.Sprint(i), 1), 0))
	}
	before := db.manager.db.MaxVersion()
	result, err := db.ForceSyncBatch(context.Background(), 128)
	require.NoError(t, err)
	require.Equal(t, 128, result.Confirmed)
	require.Zero(t, db.GetCachedPendingSyncCount())
	commits := db.manager.db.MaxVersion() - before
	t.Logf("128条确认的Badger真实提交事务数=%d", commits)
	require.LessOrEqual(t, commits, uint64(5), "选批一次+有界确认块，不得逐条可靠提交")
}

// TestConfirmationYieldsSmallQuantumToForeground 已接收前台存在时首个确认块最多8项，清理入队屏障固定真实块大小。
func TestConfirmationYieldsSmallQuantumToForeground(t *testing.T) {
	store := openCleanupTestStore(t, t.TempDir(), false, true, &cleanupTestTarget{})
	db := store.db
	db.manager.config.SyncBatchSize = 32
	db.disposition.policy.ScanLimit = 32
	for i := range 32 {
		require.NoError(t, store.Save(context.Background(), newCleanupTestRecord(t, fmt.Sprint(i), 1, false, &cleanupTestProbe{})))
	}
	selected, _, err := db.selectDispositionRound(32)
	require.NoError(t, err)
	require.Len(t, selected, 32)
	success := map[string]bool{}
	for _, r := range selected {
		success[r.Snapshot.Key] = true
	}
	entered, release := make(chan struct{}), make(chan struct{})
	original := store.batcher.commit
	store.batcher.commit = func(ops []WriteOperation[cleanupTestRecord]) (BatchWriteResult, error) {
		close(entered)
		<-release
		return original(ops)
	}
	front := make(chan error, 1)
	pressure := newCleanupTestRecord(t, "pressure", 1, false, &cleanupTestProbe{})
	go func() { front <- store.Save(context.Background(), pressure) }()
	<-entered
	db.sadQueueMu.Lock()
	var unlocked sync.Once
	defer unlocked.Do(db.sadQueueMu.Unlock)
	result := make(chan error, 1)
	go func() { _, _, e := db.commitDispositionResults(selected, success, nil); result <- e }()
	require.Eventually(t, func() bool { return len(store.batcher.requests) > 0 }, time.Second, time.Millisecond)
	close(release)
	require.NoError(t, <-front)
	require.Eventually(t, func() bool { return db.GetSyncMetrics().ConfirmationLocal.Committed > 0 }, time.Second, time.Millisecond)
	first := db.GetSyncMetrics().ConfirmationLocal.Items
	unlocked.Do(db.sadQueueMu.Unlock)
	require.NoError(t, <-result)
	require.LessOrEqual(t, first, uint64(8), "有前台请求时不能把32项大块等待搬到FIFO")
}

// TestConfirmationBlockRealCommitConflict 多条原件的真实Badger提交冲突必须整体回滚，不扣pending或提前执行hook。
func TestConfirmationBlockRealCommitConflict(t *testing.T) {
	probe := &cleanupTestProbe{}
	store := openCleanupTestStore(t, t.TempDir(), false, true, &cleanupTestTarget{})
	db := store.db
	for i := range 4 {
		require.NoError(t, store.Save(context.Background(), newCleanupTestRecord(t, fmt.Sprint(i), 1, true, probe)))
	}
	selected, _, err := db.selectDispositionRound(4)
	require.NoError(t, err)
	require.Len(t, selected, 4)
	err = db.updateWriteBehindTransaction(func(txn *badger.Txn) error {
		groups := map[string]struct{}{}
		for _, r := range selected {
			r := r
			removed, e := db.completeDisposition(txn, &r, WriteBehindCompleted, groups)
			if e != nil {
				return e
			}
			require.True(t, removed)
		}
		if e := db.deleteEmptyDispositionGroups(txn, groups); e != nil {
			return e
		}
		// 与既有真实冲突回归相同，另一个事务触碰本事务已读的索引，确保Commit而非闭包失败。
		return db.manager.db.Update(func(other *badger.Txn) error {
			return other.Set([]byte(db.syncQueueKey(selected[0].Snapshot.Key)), nil)
		})
	})
	require.ErrorIs(t, err, badger.ErrConflict)
	require.Equal(t, 4, db.GetCachedPendingSyncCount())
	require.Zero(t, probe.calls.Load())
	for _, r := range selected {
		v, e := db.getWrapper(r.Snapshot.Key)
		require.NoError(t, e)
		require.False(t, v.IsSynced)
	}
	success := map[string]bool{}
	for _, r := range selected {
		success[r.Snapshot.Key] = true
	}
	n, _, err := db.commitDispositionResults(selected, success, nil)
	require.NoError(t, err)
	require.Equal(t, 4, n)
	require.Eventually(t, func() bool { return probe.calls.Load() == 4 }, time.Second, time.Millisecond)
	require.Zero(t, db.GetCachedPendingSyncCount())
}

// TestLocalCommitCloseWaitsForRealResult 关闭超时不能丢弃已接受本地命令，原worker完成后有真实结果且退出。
func TestLocalCommitCloseWaitsForRealResult(t *testing.T) {
	db := openFailureTestDB(t, t.TempDir(), false, &failureTestTarget{})
	b := newBatchCommitter[testFund](BatchCommitConfig{MaxBatch: 2, QueueCapacity: 2}, db.ApplyWriteOperations)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }); _ = b.Close(context.Background()) })
	done := make(chan error, 1)
	go func() {
		done <- b.submitLocalCommit(func() error {
			close(entered)
			<-release
			return db.manager.db.Update(func(txn *badger.Txn) error { return txn.Set([]byte("local-result"), []byte("durable")) })
		})
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, b.Close(ctx), context.DeadlineExceeded)
	select {
	case <-done:
		t.Fatal("已接受命令未提交就返回")
	default:
	}
	once.Do(func() { close(release) })
	require.NoError(t, <-done)
	select {
	case <-b.done:
	case <-time.After(time.Second):
		t.Fatal("提交worker未退出")
	}
	require.ErrorIs(t, b.submitLocalCommit(func() error { return nil }), ErrWriteStoreClosed)
	require.NoError(t, db.manager.db.View(func(txn *badger.Txn) error { _, e := txn.Get([]byte("local-result")); return e }))
}

// TestBoundedLocalConfirmationRespectsAcceptedQueue 固定已接受前台在提交中的屏障，后台确认不能绕开现有FIFO提交面。
func TestBoundedLocalConfirmationRespectsAcceptedQueue(t *testing.T) {
	targetEntered, targetRelease := make(chan struct{}), make(chan struct{})
	frontEntered, frontRelease := make(chan struct{}), make(chan struct{})
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		close(targetEntered)
		<-targetRelease
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}}
	store := openCleanupTestStore(t, t.TempDir(), false, true, target)
	original := store.batcher.commit
	store.batcher.commit = func(ops []WriteOperation[cleanupTestRecord]) (BatchWriteResult, error) {
		if ops[0].Item.Key == "front" {
			close(frontEntered)
			<-frontRelease
		}
		return original(ops)
	}
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(frontRelease) }) })
	require.NoError(t, store.Save(context.Background(), newCleanupTestRecord(t, "original", 1, false, &cleanupTestProbe{})))
	background := make(chan error, 1)
	go func() { _, e := store.ForceSyncBatch(context.Background(), 16); background <- e }()
	<-targetEntered
	front := make(chan error, 1)
	go func() {
		front <- store.Save(context.Background(), newCleanupTestRecord(t, "front", 1, false, &cleanupTestProbe{}))
	}()
	<-frontEntered
	close(targetRelease)
	require.Eventually(t, func() bool {
		select {
		case err := <-background:
			t.Fatalf("后台绕过已接受前台提交：%v", err)
		default:
		}
		return len(store.batcher.requests) > 0
	}, time.Second, time.Millisecond)
	once.Do(func() { close(frontRelease) })
	require.NoError(t, <-front)
	require.NoError(t, <-background)
}

// TestDispositionConfirmationSplitsActualTransactionSize 使用真实小MemTable使写集超过Badger硬限，不用Fake事务错误。
func TestDispositionConfirmationSplitsActualTransactionSize(t *testing.T) {
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.EnableLogger = false
	cfg.MemTableSize = 1 << 20
	cfg.AutoSync = false
	cfg.WriteBehindFailure = failureTestPolicy()
	cfg.WriteBehindFailure.ScanLimit = 16
	db, err := NewSharedBadgerDB[confirmationLoadRecord](path, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); _ = CloseSharedManager(path) })
	a := &confirmationLoadTarget{}
	require.NoError(t, db.UseWriteBehindWithFailureHandler(a, a))
	var selected []WriteBehindFailureRecord[confirmationLoadRecord]
	success := map[string]bool{}
	for i := range 8 {
		r := &confirmationLoadRecord{Model: entity.NewModel(), Key: fmt.Sprint(i), Group: i, Payload: bytes.Repeat([]byte("x"), 10000)}
		require.NoError(t, db.Set(r, 0))
		var one []WriteBehindFailureRecord[confirmationLoadRecord]
		for attempt := 0; attempt < 3 && len(one) == 0; attempt++ {
			one, _, err = db.selectDispositionRound(1)
			require.NoError(t, err)
		}
		require.Len(t, one, 1)
		selected = append(selected, one[0])
		success[one[0].Snapshot.Key] = true
	}
	for _, r := range selected {
		r := r
		require.NoError(t, db.updateWriteBehindTransaction(func(txn *badger.Txn) error { return db.saveDisposition(txn, &r) }))
	}
	n, notice, err := db.commitDispositionResults(selected, success, nil)
	require.NoError(t, err)
	require.Equal(t, 8, n)
	require.Empty(t, notice)
	m := db.GetSyncMetrics().ConfirmationLocal
	require.Greater(t, m.TooBig, uint64(0), "必须真实观察到ErrTxnTooBig")
	require.Equal(t, uint64(8), m.Items)
	require.Greater(t, m.Committed, uint64(1))
	require.Zero(t, db.GetCachedPendingSyncCount())
	t.Logf("实际过大=%d，拆分后提交=%d，items=%d，Badger硬限=%d", m.TooBig, m.Committed, m.Items, db.manager.db.MaxBatchSize())
}

// TestDispositionConfirmationPartialBlocks 本地第二块写集失败时，首块确认及hook保持，未提交块不能提前扣数或回收。
func TestDispositionConfirmationPartialBlocks(t *testing.T) {
	store := openCleanupTestStore(t, t.TempDir(), false, true, &cleanupTestTarget{})
	db := store.db
	db.manager.config.SyncBatchSize = 64
	db.disposition.policy.ScanLimit = 64
	probe := &cleanupTestProbe{}
	for i := range 64 {
		require.NoError(t, store.Save(context.Background(), newCleanupTestRecord(t, fmt.Sprintf("key-%03d", i), 1, true, probe)))
	}
	selected, _, err := db.selectDispositionRound(64)
	require.NoError(t, err)
	require.Len(t, selected, 64)
	bad := db.dispositionPrefix() + "r:" + selected[63].ID
	var saved []byte
	require.NoError(t, db.manager.db.Update(func(txn *badger.Txn) error {
		item, e := txn.Get([]byte(bad))
		if e != nil {
			return e
		}
		saved, e = item.ValueCopy(nil)
		if e != nil {
			return e
		}
		return txn.Set([]byte(bad), []byte("broken-json"))
	}))
	success := map[string]bool{}
	for _, r := range selected {
		success[r.Snapshot.Key] = true
	}
	n, notice, err := db.commitDispositionResults(selected, success, nil)
	require.Error(t, err)
	require.Equal(t, 32, n)
	require.Empty(t, notice)
	require.Equal(t, 32, db.GetCachedPendingSyncCount())
	require.Eventually(t, func() bool { return probe.calls.Load() == 32 }, time.Second, time.Millisecond)
	for _, r := range selected[32:] {
		v, e := db.getWrapper(r.Snapshot.Key)
		require.NoError(t, e)
		require.False(t, v.IsSynced)
	}
	// 恢复损坏注入的原始字节后，仍按原版本/次数确认剩余块，不重调target。
	require.NoError(t, db.manager.db.Update(func(txn *badger.Txn) error { return txn.Set([]byte(bad), saved) }))
	n, _, err = db.commitDispositionResults(selected[32:], success, nil)
	require.NoError(t, err)
	require.Equal(t, 32, n)
	require.Eventually(t, func() bool { return probe.calls.Load() == 64 }, time.Second, time.Millisecond)
	require.Zero(t, db.GetCachedPendingSyncCount())
}

// TestDispositionConfirmationStaleEntryIsolation 一个原状态/指纹失效不能确认它，也不能回滚同块其他有效项。
func TestDispositionConfirmationStaleEntryIsolation(t *testing.T) {
	a := &failureTestTarget{}
	db := openFailureTestDB(t, t.TempDir(), false, a)
	for i := range 4 {
		require.NoError(t, db.Set(newFund(fmt.Sprint(i), fmt.Sprint(i), 1), 0))
	}
	selected, _, err := db.selectDispositionRound(4)
	require.NoError(t, err)
	require.NoError(t, db.updateWriteBehindTransaction(func(txn *badger.Txn) error {
		r := selected[0]
		r.Fingerprint = "changed"
		return db.saveDisposition(txn, &r)
	}))
	success := map[string]bool{}
	for _, r := range selected {
		success[r.Snapshot.Key] = true
	}
	n, _, err := db.commitDispositionResults(selected, success, nil)
	require.ErrorIs(t, err, ErrWriteBehindDisposition)
	require.Equal(t, 3, n)
	require.Equal(t, 1, db.GetCachedPendingSyncCount())
	n, _, err = db.commitDispositionResults(selected[1:], success, nil)
	require.ErrorIs(t, err, ErrWriteBehindDisposition)
	require.Zero(t, n)
	require.Equal(t, 1, db.GetCachedPendingSyncCount())
}

// TestDispositionForegroundAndBackgroundProgress 验证真实自动worker与持续Save均前进，停止前台后自行排空，没有第二套提交队列。
func TestDispositionForegroundAndBackgroundProgress(t *testing.T) {
	var targets atomic.Int64
	a := &failureTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		targets.Add(1)
		r := &WriteBehindResult{}
		for _, v := range items {
			r.ConfirmedKeys = append(r.ConfirmedKeys, v.Key)
		}
		return r, nil
	}}
	db := openFailureTestDB(t, t.TempDir(), true, a)
	for i := range 256 {
		require.NoError(t, db.Set(newFund(fmt.Sprint(i), fmt.Sprint(i%16), 1), 0))
		if i == 128 {
			require.Eventually(t, func() bool { return targets.Load() > 0 && db.GetSyncMetrics().SyncedItems > 0 }, time.Second, time.Millisecond)
		}
	}
	require.Eventually(t, func() bool { return db.GetCachedPendingSyncCount() == 0 && db.GetSyncMetrics().SyncedItems == 256 }, 3*time.Second, time.Millisecond)
	m := db.GetSyncMetrics()
	require.Greater(t, m.ForegroundLocal.Committed+m.MetadataLocal.Committed, uint64(0))
	require.Greater(t, m.ConfirmationLocal.Committed, uint64(0))
	require.LessOrEqual(t, m.ConfirmationLocal.MaxItems, 32)
}
