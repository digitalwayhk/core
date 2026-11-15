// 本文件覆盖独立组执行的真实确认、未知结果、版本、预算和关闭边界；不模拟持久化成功。
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

func openIndependentStore(t *testing.T, path string, auto bool, workers int, target *cleanupTestTarget) *ReliableWriteStore[cleanupTestRecord] {
	t.Helper()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = auto
	cfg.EnableLogger = false
	cfg.SyncBatchSize = 16
	cfg.WriteBehindFailure = failureTestPolicy()
	cfg.WriteBehindFailure.ScanLimit = 16
	cfg.SyncFlushThreshold = 4
	cfg.SyncMaxCollectDelay = time.Millisecond
	cfg.SyncBacklogDrainDelay = time.Millisecond
	s, _, e := NewReliableWriteStore[cleanupTestRecord](ServiceIdentity{ServiceName: "group-test", DataCenterID: 1, MachineID: 2}, ReliableWriteStoreConfig{BasePath: path, Badger: cfg, CloseTimeout: time.Second})
	require.NoError(t, e)
	t.Cleanup(func() { _ = s.Close(context.Background()); _ = CloseSharedManager(s.config.BasePath) })
	require.NoError(t, s.UseWriteBehindWithFailureHandlerOptions(target, target, WriteBehindExecutionOptions{IndependentGroups: workers}))
	return s
}
func groupRecord(t *testing.T, key, group string, revision int, deleteAfter bool, probe *cleanupTestProbe) *cleanupTestRecord {
	t.Helper()
	r := newCleanupTestRecord(t, key, revision, deleteAfter, probe)
	r.Group = group
	return r
}
func awaitGroupChannel(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("未到达受控执行点")
	}
}

// TestIndependentGroupsAutoDrain 验证自动worker无需第二次ForceSync或新Save继续健康组排空。
func TestIndependentGroupsAutoDrain(t *testing.T) {
	entered, release, next := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		switch items[0].Item.Key {
		case "slow":
			close(entered)
			<-release
		case "next":
			close(next)
		}
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}}
	s := openIndependentStore(t, t.TempDir(), true, 2, target)
	defer unblock()
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "slow", "A", 1, false, &cleanupTestProbe{})))
	awaitGroupChannel(t, entered)
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "first", "B", 1, true, &cleanupTestProbe{})))
	require.Eventually(t, func() bool { return s.Metrics().Sync.SyncedItems == 1 }, time.Second, time.Millisecond)
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "next", "B", 1, true, &cleanupTestProbe{})))
	awaitGroupChannel(t, next)
	require.Eventually(t, func() bool { return s.Metrics().Pending == 1 }, time.Second, time.Millisecond)
	// pending变更与worker释放是两个边界；必须等B的原worker实际归还槽，不能把刚确认瞬间当退出。
	require.Eventually(t, func() bool {
		m, e := s.WriteBehindDispositionMetrics()
		return e == nil && m.TargetActive == 1 && m.TargetQueued == 0
	}, time.Second, time.Millisecond)
	m, e := s.WriteBehindDispositionMetrics()
	require.NoError(t, e)
	require.Equal(t, 1, m.TargetActive)
	require.Equal(t, 2, m.TargetLimit)
	require.Zero(t, m.HandlerActive)
	unblock()
	require.Eventually(t, func() bool { return s.Metrics().Pending == 0 }, time.Second, time.Millisecond)
	require.EqualValues(t, 3, s.Metrics().Sync.SyncedItems)
}

// TestIndependentGroupsUnknownBarrier 验证未确认前序进入原失败处置；健康组继续，原组后继不执行。
func TestIndependentGroupsUnknownBarrier(t *testing.T) {
	var later, healthy atomic.Int32
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		switch items[0].Item.Key {
		case "unknown":
			return nil, errors.New("受控未知结果")
		case "later":
			later.Add(1)
		case "healthy":
			healthy.Add(1)
		}
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}}
	s := openIndependentStore(t, t.TempDir(), true, 2, target)
	for _, v := range []*cleanupTestRecord{groupRecord(t, "unknown", "A", 1, true, &cleanupTestProbe{}), groupRecord(t, "later", "A", 1, false, &cleanupTestProbe{}), groupRecord(t, "healthy", "B", 1, false, &cleanupTestProbe{})} {
		require.NoError(t, s.Save(context.Background(), v))
	}
	r := cleanupTestAwaitState(t, s, WriteBehindPaused)
	require.Equal(t, "unknown", r.Snapshot.Item.Key)
	require.Equal(t, 1, r.Attempts)
	require.Eventually(t, func() bool { return healthy.Load() == 1 }, time.Second, time.Millisecond)
	require.Zero(t, later.Load())
	require.Equal(t, 2, s.Metrics().Pending)
	cleanupTestConfirm(t, s, r)
	require.Eventually(t, func() bool { return s.Metrics().Pending == 0 }, time.Second, time.Millisecond)
	require.Equal(t, int32(1), later.Load())
}

// TestIndependentGroupsCancelKeepsActualOwner 验证取消未退出target仍持组和槽，迟到成功不当成确认。
func TestIndependentGroupsCancelKeepsActualOwner(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var later atomic.Int32
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		if items[0].Item.Key == "slow" {
			close(entered)
			<-release
		} else if items[0].Item.Key == "later" {
			later.Add(1)
		}
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}}
	s := openIndependentStore(t, t.TempDir(), false, 2, target)
	defer once.Do(func() { close(release) })
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "slow", "A", 1, true, &cleanupTestProbe{})))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, e := s.ForceSyncBatch(ctx, 16); done <- e }()
	awaitGroupChannel(t, entered)
	cancel()
	cancelErr := <-done
	require.ErrorIs(t, cancelErr, context.Canceled)
	require.False(t, errors.Is(cancelErr, ErrWriteStoreClosed), "调用者取消不代表store已关闭")
	for _, v := range []*cleanupTestRecord{groupRecord(t, "later", "A", 1, false, &cleanupTestProbe{}), groupRecord(t, "healthy", "B", 1, false, &cleanupTestProbe{})} {
		require.NoError(t, s.Save(context.Background(), v))
	}
	r, e := s.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, e)
	require.Equal(t, 1, r.Confirmed)
	require.Zero(t, later.Load())
	records, _, e := s.ListWriteBehindFailures("", 16)
	require.NoError(t, e)
	for _, r := range records {
		if r.Snapshot.Key == s.db.prefix+"slow" {
			require.Equal(t, WriteBehindInFlight, r.State)
			require.Equal(t, 1, r.Attempts)
		}
	}
	once.Do(func() { close(release) })
	cleanupTestAwaitState(t, s, WriteBehindPaused)
	require.Equal(t, 2, s.Metrics().Pending)
	require.EqualValues(t, 1, s.Metrics().Sync.SyncedItems)
	require.Zero(t, later.Load())
}

// TestIndependentGroupsConfirmationFailure 验证本地确认失败不扣数、不触发hook，也不重做已返回的target。
func TestIndependentGroupsConfirmationFailure(t *testing.T) {
	var fail atomic.Bool
	var calls atomic.Int32
	probe := &cleanupTestProbe{}
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		calls.Add(1)
		fail.Store(true)
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}}
	s := openIndependentStore(t, t.TempDir(), false, 2, target)
	original := s.db.localSubmit
	s.db.localSubmit = func(f func() error) error {
		if fail.Swap(false) {
			return badger.ErrConflict
		}
		return original(f)
	}
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "original", "A", 1, true, probe)))
	r, e := s.ForceSyncBatch(context.Background(), 16)
	require.ErrorIs(t, e, badger.ErrConflict)
	require.Zero(t, r.Confirmed)
	require.Equal(t, 1, s.Metrics().Pending)
	require.Zero(t, probe.calls.Load())
	_, e = s.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, e)
	record := cleanupTestAwaitState(t, s, WriteBehindPaused)
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, 1, record.Attempts)
	cleanupTestConfirm(t, s, record)
	cleanupTestConfirm(t, s, record)
	cleanupTestDeleted(t, s, "original")
	require.EqualValues(t, 1, s.Metrics().Sync.SyncedItems)
	require.Equal(t, int32(1), probe.calls.Load())
}

// TestIndependentGroupsBudgetAndClose 验证固定worker占满后的背压和关闭超时，不能以取消替代实际退出。
func TestIndependentGroupsBudgetAndClose(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	var third atomic.Int32
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		if items[0].Item.Key == "third" {
			third.Add(1)
		} else {
			entered <- struct{}{}
			<-release
		}
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}}
	s := openIndependentStore(t, t.TempDir(), true, 2, target)
	defer once.Do(func() { close(release) })
	for i := 0; i < 2; i++ {
		require.NoError(t, s.Save(context.Background(), groupRecord(t, fmt.Sprint(i), fmt.Sprint(i), 1, false, &cleanupTestProbe{})))
	}
	awaitGroupChannel(t, entered)
	awaitGroupChannel(t, entered)
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "third", "C", 1, false, &cleanupTestProbe{})))
	m, e := s.WriteBehindDispositionMetrics()
	require.NoError(t, e)
	require.Equal(t, 2, m.TargetActive)
	require.LessOrEqual(t, m.TargetActive+m.TargetQueued, m.TargetLimit)
	require.Zero(t, third.Load())
	e = s.db.CloseWithTimeout(time.Millisecond, time.Millisecond)
	require.ErrorIs(t, e, ErrWriteBehindDispositionCloseTimeout)
	once.Do(func() { close(release) })
	s.db.wg.Wait()
	require.Zero(t, third.Load())
	require.Equal(t, 3, s.Metrics().Pending)
	require.Zero(t, s.Metrics().Sync.SyncedItems)
}

// TestIndependentGroupsOldVersionAndHashCollision 验证同key跨组新版本不得越过旧回调，且不同key条带碰撞不造成组间屏障。
func TestIndependentGroupsOldVersionAndHashCollision(t *testing.T) {
	for _, sameKey := range []bool{true, false} {
		t.Run(fmt.Sprint(sameKey), func(t *testing.T) {
			entered, release, next := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			old := groupRecord(t, "original", "A", 1, true, &cleanupTestProbe{})
			key := "original"
			if !sameKey {
				for i := 0; ; i++ {
					candidate := fmt.Sprint("collision-", i)
					if syncKeyLockIndex(candidate) == syncKeyLockIndex(key) {
						key = candidate
						break
					}
				}
			}
			target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
				if items[0].Item.Revision == 1 {
					close(entered)
					<-release
				} else {
					close(next)
				}
				return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
			}}
			s := openIndependentStore(t, t.TempDir(), true, 2, target)
			defer once.Do(func() { close(release) })
			require.NoError(t, s.Save(context.Background(), old))
			awaitGroupChannel(t, entered)
			require.NoError(t, s.Save(context.Background(), groupRecord(t, key, "B", 2, true, &cleanupTestProbe{})))
			if sameKey {
				select {
				case <-next:
					t.Fatal("新版本越过旧在途")
				case <-time.After(30 * time.Millisecond):
				}
			} else {
				awaitGroupChannel(t, next)
			}
			once.Do(func() { close(release) })
			require.Eventually(t, func() bool { return s.Metrics().Pending == 0 }, time.Second, time.Millisecond)
			awaitGroupChannel(t, next)
			expected := uint64(2)
			if sameKey {
				expected = 1
			}
			require.Eventually(t, func() bool { return s.Metrics().Sync.SyncedItems == expected }, time.Second, time.Millisecond)
		})
	}
}

// TestIndependentGroupsLegacyAndOptions 验证旧入口/零值不启用并发，非法数量拒绝，持久policy字节不受执行选项影响。
func TestIndependentGroupsLegacyAndOptions(t *testing.T) {
	for _, n := range []int{-1, 65} {
		require.ErrorIs(t, (WriteBehindExecutionOptions{IndependentGroups: n}).validate(), ErrWriteBehindDisposition)
	}
	for _, n := range []int{0, 1, 64} {
		require.NoError(t, (WriteBehindExecutionOptions{IndependentGroups: n}).validate())
	}
	path := t.TempDir()
	target := &cleanupTestTarget{}
	s := openIndependentStore(t, path, false, 0, target)
	require.Nil(t, s.db.disposition.groups)
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "saved", "A", 1, false, &cleanupTestProbe{})))
	var pending *PendingSyncError
	require.ErrorAs(t, s.Close(context.Background()), &pending)
	require.NoError(t, CloseSharedManager(s.config.BasePath))
	s = openIndependentStore(t, path, false, 2, target)
	r, e := s.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, e)
	require.Equal(t, 1, r.Confirmed)
}

// TestIndependentGroupsPartialErrorStillAdvances 验证成功加error仍按真实确认推进后继，不吞原错误或误调失败handler。
func TestIndependentGroupsPartialErrorStillAdvances(t *testing.T) {
	original := errors.New("部分成功的原错误")
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, original
	}, handle: func(context.Context, WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error) {
		t.Error("已确认原件不可调失败handler")
		return WriteBehindDecision{}, nil
	}}
	s := openIndependentStore(t, t.TempDir(), false, 2, target)
	for i := 0; i < 2; i++ {
		require.NoError(t, s.Save(context.Background(), groupRecord(t, fmt.Sprint(i), "same", 1, false, &cleanupTestProbe{})))
		r, e := s.ForceSyncBatch(context.Background(), 16)
		require.ErrorIs(t, e, original)
		require.Equal(t, 1, r.Confirmed)
	}
	require.Zero(t, s.Metrics().Pending)
	require.EqualValues(t, 2, s.Metrics().Sync.SyncedItems)
	require.EqualValues(t, 2, s.Metrics().Sync.Failures)
}

// TestIndependentGroupsBatchAndCauseIdentity 验证每次独立target自己的BatchID/未知集合和原error不能套给另一个组。
func TestIndependentGroupsBatchAndCauseIdentity(t *testing.T) {
	causeA, causeB := errors.New("A原尝试"), errors.New("B原尝试")
	var handled atomic.Int32
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		if items[0].Item.Group == "A" {
			return nil, causeA
		}
		return nil, causeB
	}}
	target.handle = func(_ context.Context, f WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error) {
		require.Len(t, f.Unconfirmed, 1)
		require.Equal(t, f.Record.ID, f.Unconfirmed[0].ID)
		expected := causeA
		if f.Record.Group == "B" {
			expected = causeB
		}
		require.ErrorIs(t, f.Cause, expected)
		handled.Add(1)
		return cleanupTestDecision(f.Record, WriteBehindPause), nil
	}
	s := openIndependentStore(t, t.TempDir(), true, 2, target)
	for _, g := range []string{"A", "B"} {
		require.NoError(t, s.Save(context.Background(), groupRecord(t, g, g, 1, false, &cleanupTestProbe{})))
	}
	require.Eventually(t, func() bool { return handled.Load() == 2 }, time.Second, time.Millisecond)
	records, _, e := s.ListWriteBehindFailures("", 16)
	require.NoError(t, e)
	require.Len(t, records, 2)
	require.NotEqual(t, records[0].BatchID, records[1].BatchID)
	require.Equal(t, 2, s.Metrics().Pending)
}

// TestIndependentGroupsFailureCallbackMustExit 验证失败回调超时未退出时人工Retry也不能重入原组，健康组仍推进。
func TestIndependentGroupsFailureCallbackMustExit(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var attempts atomic.Int32
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		if items[0].Item.Group == "A" && attempts.Add(1) == 1 {
			return nil, errors.New("原结果未知")
		}
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}}
	target.handle = func(_ context.Context, f WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error) {
		close(entered)
		<-release
		d := cleanupTestDecision(f.Record, WriteBehindConfirm)
		d.Outcome = WriteBehindCommitted
		return d, nil
	}
	s := openIndependentStore(t, t.TempDir(), true, 2, target)
	defer once.Do(func() { close(release) })
	// 预算在启动前的配置已冻结，此处保留原一秒超时，不为测试改policy。
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "failed", "A", 1, false, &cleanupTestProbe{})))
	awaitGroupChannel(t, entered)
	var record WriteBehindFailureRecord[cleanupTestRecord]
	require.Eventually(t, func() bool {
		r, _, e := s.ListWriteBehindFailures("", 16)
		if e == nil && len(r) == 1 && r[0].State == WriteBehindPaused {
			record = r[0]
			return true
		}
		return false
	}, 3*time.Second, time.Millisecond)
	d := cleanupTestDecision(record, WriteBehindRetry)
	d.Outcome = WriteBehindNotCommitted
	d.RetryAfter = s.db.disposition.policy.MinRetryInterval
	require.NoError(t, s.ApplyWriteBehindDecision(context.Background(), d))
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "healthy-after-timeout", "B", 1, false, &cleanupTestProbe{})))
	require.Eventually(t, func() bool { return s.Metrics().Sync.SyncedItems == 1 }, time.Second, time.Millisecond)
	require.Equal(t, int32(1), attempts.Load())
	once.Do(func() { close(release) })
	require.Eventually(t, func() bool { return s.Metrics().Pending == 0 }, 2*time.Second, time.Millisecond)
	require.Equal(t, int32(2), attempts.Load())
}

// TestIndependentGroupsForcedDeleteRemainsExclusive 验证条带共享不放松本地强制删除与target的原互斥。
func TestIndependentGroupsForcedDeleteRemainsExclusive(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		close(entered)
		<-release
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}}
	s := openIndependentStore(t, t.TempDir(), true, 2, target)
	defer once.Do(func() { close(release) })
	record := groupRecord(t, "protected", "A", 1, false, &cleanupTestProbe{})
	require.NoError(t, s.Save(context.Background(), record))
	awaitGroupChannel(t, entered)
	done := make(chan error, 1)
	go func() { done <- s.db.ForceDeleteLocal(record) }()
	select {
	case <-done:
		t.Fatal("强制删除越过实际在途target")
	case <-time.After(20 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	require.NoError(t, <-done)
	_, e := s.GetLocal(context.Background(), record.Key)
	require.ErrorIs(t, e, badger.ErrKeyNotFound)
}

// TestIndependentGroupsSelectionTooBigSplits 验证真实选批过大回滚后缩小写集，原attempt不重复消费。
func TestIndependentGroupsSelectionTooBigSplits(t *testing.T) {
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = false
	cfg.EnableLogger = false
	cfg.MemTableSize = 1 << 20
	cfg.SyncBatchSize = 16
	cfg.SyncFlushThreshold = 4
	cfg.SyncMaxCollectDelay = time.Millisecond
	cfg.WriteBehindFailure = failureTestPolicy()
	cfg.WriteBehindFailure.ScanLimit = 16
	target := &selectionLoadTarget{}
	store, _, e := NewReliableWriteStore[selectionLoadRecord](ServiceIdentity{ServiceName: "group-too-big"}, ReliableWriteStoreConfig{BasePath: path, Badger: cfg})
	require.NoError(t, e)
	t.Cleanup(func() { _ = store.Close(context.Background()); _ = CloseSharedManager(store.config.BasePath) })
	require.NoError(t, store.UseWriteBehindWithFailureHandlerOptions(target, target, WriteBehindExecutionOptions{IndependentGroups: 8}))
	items := []*selectionLoadRecord{}
	for i := range 8 {
		r := selectionRecord(fmt.Sprint(i), i)
		r.Payload = make([]byte, 20000)
		items = append(items, r)
	}
	n, e := store.SaveBatch(context.Background(), items)
	require.NoError(t, e)
	require.Equal(t, 8, n.Committed)
	r, e := store.ForceSyncAll(context.Background())
	require.NoError(t, e)
	require.Equal(t, 8, r.Confirmed)
	require.Zero(t, r.Remaining)
	records, _, e := store.ListWriteBehindFailures("", 16)
	require.NoError(t, e)
	require.Len(t, records, 8)
	for _, r := range records {
		require.Equal(t, 1, r.Attempts)
		require.True(t, r.LocalConfirmed)
	}
	require.Greater(t, store.Metrics().Sync.MetadataLocal.TooBig, uint64(0), "必须实际触发Badger ErrTxnTooBig")
}

// TestIndependentGroupsManualDecisionWakesWaiter 验证人工确认/新写通知能唤醒等空闲健康组的手动调用，不能继续等无关A。
func TestIndependentGroupsManualDecisionWakesWaiter(t *testing.T) {
	aEntered, aRelease, cEntered, cRelease, bNext := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var onceA, onceC sync.Once
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		switch items[0].Item.Key {
		case "A":
			close(aEntered)
			<-aRelease
		case "B1":
			return nil, errors.New("B原结果待应用确认")
		case "C":
			close(cEntered)
			<-cRelease
		case "B2":
			close(bNext)
		}
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}}
	s := openIndependentStore(t, t.TempDir(), false, 2, target)
	defer onceA.Do(func() { close(aRelease) })
	defer onceC.Do(func() { close(cRelease) })
	for _, r := range []*cleanupTestRecord{groupRecord(t, "A", "A", 1, false, &cleanupTestProbe{}), groupRecord(t, "B1", "B", 1, false, &cleanupTestProbe{})} {
		require.NoError(t, s.Save(context.Background(), r))
	}
	first := make(chan error, 1)
	go func() { _, e := s.ForceSyncBatch(context.Background(), 16); first <- e }()
	awaitGroupChannel(t, aEntered)
	record := cleanupTestAwaitState(t, s, WriteBehindPaused)
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "B2", "B", 1, false, &cleanupTestProbe{})))
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "C", "C", 1, false, &cleanupTestProbe{})))
	cDone := make(chan error, 1)
	go func() { _, e := s.ForceSyncBatch(context.Background(), 16); cDone <- e }()
	awaitGroupChannel(t, cEntered)
	// 唯一空闲worker必须已完成B并处理C；精确等待C最后一次池通知，排除旧通知偶然唤醒。
	changed := s.db.disposition.groups.changeSignal()
	onceC.Do(func() { close(cRelease) })
	<-changed
	require.NoError(t, <-cDone)
	scanned, continueScan := make(chan struct{}), make(chan struct{})
	var scanOnce sync.Once
	s.db.disposition.selectionRoundObserver = func(time.Duration) { scanOnce.Do(func() { close(scanned); <-continueScan }) }
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	waiter := make(chan error, 1)
	go func() { _, e := s.ForceSyncBatch(ctx, 16); waiter <- e }()
	awaitGroupChannel(t, scanned)
	// 保证决策通知发生在选批结束与实际等待之间；不能丢唤醒。
	cleanupTestConfirm(t, s, record)
	close(continueScan)
	select {
	case <-bNext:
	case <-ctx.Done():
		t.Fatal("应用已确认B1，但手动同步仍等待无关A而未进入B2")
	}
	require.NoError(t, <-waiter)
	onceA.Do(func() { close(aRelease) })
	require.Error(t, <-first)
}

// TestIndependentGroupsManualSaveWakesWaiter 验证等待期间新Save产生健康组时，由原通知推进而不依赖A结束。
func TestIndependentGroupsManualSaveWakesWaiter(t *testing.T) {
	entered, release, next := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		if items[0].Item.Group == "A" {
			close(entered)
			<-release
		} else {
			close(next)
		}
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}}
	s := openIndependentStore(t, t.TempDir(), false, 2, target)
	defer once.Do(func() { close(release) })
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "A", "A", 1, false, &cleanupTestProbe{})))
	first := make(chan error, 1)
	go func() { _, e := s.ForceSyncBatch(context.Background(), 16); first <- e }()
	awaitGroupChannel(t, entered)
	scanned, continueScan := make(chan struct{}), make(chan struct{})
	var scannedOnce sync.Once
	s.db.disposition.selectionRoundObserver = func(time.Duration) { scannedOnce.Do(func() { close(scanned); <-continueScan }) }
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	waiter := make(chan error, 1)
	go func() { _, e := s.ForceSyncBatch(ctx, 16); waiter <- e }()
	awaitGroupChannel(t, scanned)
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "B", "B", 1, false, &cleanupTestProbe{})))
	close(continueScan)
	select {
	case <-next:
	case <-ctx.Done():
		t.Fatal("新Save的健康组未唤醒手动等待者")
	}
	require.NoError(t, <-waiter)
	once.Do(func() { close(release) })
	require.NoError(t, <-first)
}
