// 本文件以真实Badger确认写集验证FIFO边界、合批失败隔离和入队后取消；不把target返回当本地确认。
package nosql

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

type convoyConfirmationResult struct {
	n   int
	err error
}

func prepareConvoyConfirmations(t *testing.T, n int) (*ReliableWriteStore[cleanupTestRecord], []WriteBehindFailureRecord[cleanupTestRecord]) {
	t.Helper()
	s := openIndependentStore(t, t.TempDir(), false, 32, &cleanupTestTarget{}, func(c *BadgerDBConfig) { c.SyncBatchSize = 64; c.WriteBehindFailure.ScanLimit = 64 })
	for i := 0; i < n; i++ {
		require.NoError(t, s.Save(context.Background(), groupRecord(t, fmt.Sprint(i), fmt.Sprint(i), 1, false, &cleanupTestProbe{})))
	}
	records, notify, e := s.db.selectDispositionRound(n)
	require.NoError(t, e)
	require.Len(t, records, n)
	require.Empty(t, notify)
	return s, records
}

func blockConvoyFIFO(t *testing.T, s *ReliableWriteStore[cleanupTestRecord]) func() {
	t.Helper()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	result, e := s.batcher.queueLocalCommit(context.Background(), func() error { close(entered); <-release; return nil })
	require.NoError(t, e)
	awaitGroupChannel(t, entered)
	unblock := func() { once.Do(func() { close(release); require.NoError(t, (<-result).err) }) }
	t.Cleanup(unblock)
	return unblock
}
func queueConvoyConfirmation(s *ReliableWriteStore[cleanupTestRecord], r WriteBehindFailureRecord[cleanupTestRecord], ctx context.Context) <-chan convoyConfirmationResult {
	done := make(chan convoyConfirmationResult, 1)
	go func() {
		n, _, e := s.db.commitDispositionResults([]WriteBehindFailureRecord[cleanupTestRecord]{r}, map[string]bool{r.Snapshot.Key: true}, nil, ctx)
		done <- convoyConfirmationResult{n, e}
	}()
	return done
}
func awaitConvoyQueue(t *testing.T, s *ReliableWriteStore[cleanupTestRecord], n int) {
	t.Helper()
	require.Eventually(t, func() bool { return len(s.batcher.requests) == n }, time.Second, time.Millisecond)
}

// TestIndependentConfirmationFIFOFrontier 前台保存是确认合批边界，前后两份确认不可越过它合并或重排。
func TestIndependentConfirmationFIFOFrontier(t *testing.T) {
	s, r := prepareConvoyConfirmations(t, 2)
	unblock := blockConvoyFIFO(t, s)
	first := queueConvoyConfirmation(s, r[0], context.Background())
	awaitConvoyQueue(t, s, 1)
	original := s.batcher.commit
	s.batcher.commit = func(ops []WriteOperation[cleanupTestRecord]) (BatchWriteResult, error) {
		require.NoError(t, s.db.manager.db.View(func(txn *badger.Txn) error {
			var a, b WriteBehindFailureRecord[cleanupTestRecord]
			if e := dispositionRead(txn, s.db.dispositionPrefix()+"r:"+r[0].ID, &a); e != nil {
				return e
			}
			if e := dispositionRead(txn, s.db.dispositionPrefix()+"r:"+r[1].ID, &b); e != nil {
				return e
			}
			require.Equal(t, WriteBehindCompleted, a.State)
			require.Equal(t, WriteBehindInFlight, b.State)
			return nil
		}))
		return original(ops)
	}
	saved := make(chan error, 1)
	go func() {
		saved <- s.Save(context.Background(), groupRecord(t, "new", "C", 1, false, &cleanupTestProbe{}))
	}()
	awaitConvoyQueue(t, s, 2)
	second := queueConvoyConfirmation(s, r[1], context.Background())
	awaitConvoyQueue(t, s, 3)
	unblock()
	require.NoError(t, (<-first).err)
	require.NoError(t, <-saved)
	require.NoError(t, (<-second).err)
	require.EqualValues(t, 2, s.Metrics().Sync.ConfirmationLocal.Committed)
	require.Equal(t, 1, s.Metrics().Pending, "新增的前台事实仍待同步")
}

// TestIndependentConfirmationQueuedCancellation 取消发生在入队后，实际事务必须保留该项Unknown，健康邻项可同批确认。
func TestIndependentConfirmationQueuedCancellation(t *testing.T) {
	s, r := prepareConvoyConfirmations(t, 2)
	unblock := blockConvoyFIFO(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := queueConvoyConfirmation(s, r[0], ctx)
	awaitConvoyQueue(t, s, 1)
	second := queueConvoyConfirmation(s, r[1], context.Background())
	awaitConvoyQueue(t, s, 2)
	cancel()
	unblock()
	a, b := <-first, <-second
	require.ErrorIs(t, a.err, context.Canceled)
	require.Zero(t, a.n)
	require.NoError(t, b.err)
	require.Equal(t, 1, b.n)
	require.Equal(t, 1, s.Metrics().Pending)
	require.EqualValues(t, 1, s.Metrics().Sync.ConfirmationLocal.Committed)
	require.NoError(t, s.db.manager.db.View(func(txn *badger.Txn) error {
		var current WriteBehindFailureRecord[cleanupTestRecord]
		e := dispositionRead(txn, s.db.dispositionPrefix()+"r:"+r[0].ID, &current)
		require.Equal(t, WriteBehindUnknown, current.State)
		require.Equal(t, 1, current.Attempts)
		return e
	}))
}

// TestIndependentConfirmationMergedRollback 第二项坏生命周期引发真实事务错误，第一项写集也必须回滚、计数不提前发布。
func TestIndependentConfirmationMergedRollback(t *testing.T) {
	s, r := prepareConvoyConfirmations(t, 2)
	key := s.db.dispositionPrefix() + "r:" + r[1].ID
	require.NoError(t, s.db.manager.db.Update(func(txn *badger.Txn) error { return txn.Set([]byte(key), []byte("{")) }))
	unblock := blockConvoyFIFO(t, s)
	first := queueConvoyConfirmation(s, r[0], context.Background())
	awaitConvoyQueue(t, s, 1)
	second := queueConvoyConfirmation(s, r[1], context.Background())
	awaitConvoyQueue(t, s, 2)
	unblock()
	a, b := <-first, <-second
	require.Error(t, a.err)
	require.Error(t, b.err)
	require.Zero(t, a.n)
	require.Zero(t, b.n)
	require.Equal(t, 2, s.Metrics().Pending)
	require.NoError(t, s.db.manager.db.View(func(txn *badger.Txn) error {
		var current WriteBehindFailureRecord[cleanupTestRecord]
		e := dispositionRead(txn, s.db.dispositionPrefix()+"r:"+r[0].ID, &current)
		require.Equal(t, WriteBehindInFlight, current.State)
		return e
	}))
}

// TestIndependentConfirmationSemanticIsolation 旧指纹只拒绝该命令，不使另一份有效确认丢失或跨store确认。
func TestIndependentConfirmationSemanticIsolation(t *testing.T) {
	s, r := prepareConvoyConfirmations(t, 2)
	r[0].Fingerprint = "old-proof"
	unblock := blockConvoyFIFO(t, s)
	first := queueConvoyConfirmation(s, r[0], context.Background())
	awaitConvoyQueue(t, s, 1)
	second := queueConvoyConfirmation(s, r[1], context.Background())
	awaitConvoyQueue(t, s, 2)
	unblock()
	a, b := <-first, <-second
	require.ErrorIs(t, a.err, ErrWriteBehindDisposition)
	require.Zero(t, a.n)
	require.NoError(t, b.err)
	require.Equal(t, 1, b.n)
	require.Equal(t, 1, s.Metrics().Pending)
	other, _ := prepareConvoyConfirmations(t, 1)
	n, _, e := other.db.commitDispositionResults([]WriteBehindFailureRecord[cleanupTestRecord]{r[1]}, map[string]bool{r[1].Snapshot.Key: true}, nil, context.Background())
	require.Error(t, e)
	require.Zero(t, n)
	require.Equal(t, 1, other.Metrics().Pending)
}

// TestIndependentConfirmationActualTooBig 真实小memtable令相邻大原件确认触发TooBig拆块；只确认提交块，不重调target。
func TestIndependentConfirmationActualTooBig(t *testing.T) {
	entered, release := make(chan struct{}, 8), make(chan struct{})
	var once sync.Once
	unblockTargets := func() { once.Do(func() { close(release) }) }
	defer unblockTargets()
	target := &selectionLoadTarget{sync: func(_ context.Context, rows []*SyncQueueItem[selectionLoadRecord]) (*WriteBehindResult, error) {
		entered <- struct{}{}
		<-release
		return &WriteBehindResult{ConfirmedKeys: []string{rows[0].Key}}, nil
	}}
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = true
	cfg.EnableLogger = false
	cfg.MemTableSize = 1 << 20
	cfg.SyncBatchSize = 16
	cfg.SyncFlushThreshold = 4
	cfg.SyncMaxCollectDelay = time.Millisecond
	cfg.WriteBehindFailure = failureTestPolicy()
	cfg.WriteBehindFailure.ScanLimit = 16
	store, _, e := NewReliableWriteStore[selectionLoadRecord](ServiceIdentity{ServiceName: "convoy-large"}, ReliableWriteStoreConfig{BasePath: path, Badger: cfg})
	require.NoError(t, e)
	t.Cleanup(func() { _ = store.Close(context.Background()); _ = CloseSharedManager(store.config.BasePath) })
	require.NoError(t, store.UseWriteBehindWithFailureHandlerOptions(target, target, WriteBehindExecutionOptions{IndependentGroups: 8}))
	rows := make([]*selectionLoadRecord, 8)
	for i := range rows {
		rows[i] = selectionRecord(fmt.Sprint(i), i)
		rows[i].Payload = make([]byte, 20000)
	}
	r, e := store.SaveBatch(context.Background(), rows)
	require.NoError(t, e)
	require.Equal(t, 8, r.Committed)
	for range 8 {
		awaitGroupChannel(t, entered)
	}
	diskEntered, diskRelease := make(chan struct{}), make(chan struct{})
	var diskOnce sync.Once
	releaseDisk := func() { diskOnce.Do(func() { close(diskRelease) }) }
	defer releaseDisk()
	barrier, e := store.batcher.queueLocalCommit(context.Background(), func() error { close(diskEntered); <-diskRelease; return nil })
	require.NoError(t, e)
	awaitGroupChannel(t, diskEntered)
	unblockTargets()
	require.Eventually(t, func() bool { return len(store.batcher.requests) >= 8 }, time.Second, time.Millisecond)
	releaseDisk()
	require.NoError(t, (<-barrier).err)
	require.Eventually(t, func() bool { m := store.Metrics(); return m.Pending == 0 && m.Sync.SyncedItems == 8 }, 3*time.Second, time.Millisecond)
	require.Greater(t, store.Metrics().Sync.ConfirmationLocal.TooBig, uint64(0), "必须真实触发确认事务硬限")
	require.EqualValues(t, 8, target.calls.Load(), "拆本地写集不重调target")
}
