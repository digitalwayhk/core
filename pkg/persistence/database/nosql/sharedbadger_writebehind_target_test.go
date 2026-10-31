// Package nosql 验证 PrefixedBadgerDB 可插拔 WriteBehindTarget 的可靠 ACK 行为。
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
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
)

type partialFailureWriteBehindTarget[T types.IModel] struct{}

func (partialFailureWriteBehindTarget[T]) SyncBatch(_ context.Context, items []*SyncQueueItem[T]) (*WriteBehindResult, error) {
	if len(items) == 0 {
		return &WriteBehindResult{}, errors.New("远端批次失败")
	}
	return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, errors.New("远端批次部分失败")
}

const (
	writeBehindTargetEventuallyTimeout  = 3 * time.Second
	writeBehindTargetEventuallyInterval = 20 * time.Millisecond
)

type recordingWriteBehindTarget[T types.IModel] struct {
	mu          sync.Mutex
	keys        []string
	calls       int
	started     atomic.Bool
	release     chan struct{}
	blockOnce   sync.Once
	releaseOnce sync.Once
}

func newRecordingWriteBehindTarget[T types.IModel]() *recordingWriteBehindTarget[T] {
	return &recordingWriteBehindTarget[T]{release: make(chan struct{})}
}

func (tgt *recordingWriteBehindTarget[T]) SyncBatch(_ context.Context, items []*SyncQueueItem[T]) (*WriteBehindResult, error) {
	tgt.started.Store(true)
	tgt.blockOnce.Do(func() { <-tgt.release })
	tgt.mu.Lock()
	defer tgt.mu.Unlock()
	tgt.calls++
	keys := make([]string, 0, len(items))
	for _, item := range items {
		if item != nil && item.Key != "" {
			keys = append(keys, item.Key)
		}
	}
	tgt.keys = append(tgt.keys, keys...)
	return &WriteBehindResult{ConfirmedKeys: keys}, nil
}

func (tgt *recordingWriteBehindTarget[T]) startedSync() bool {
	return tgt.started.Load()
}

func (tgt *recordingWriteBehindTarget[T]) releaseSync() {
	tgt.releaseOnce.Do(func() { close(tgt.release) })
}

func (tgt *recordingWriteBehindTarget[T]) snapshot() (int, []string) {
	tgt.mu.Lock()
	defer tgt.mu.Unlock()
	return tgt.calls, append([]string(nil), tgt.keys...)
}

// TestWriteBehindTargetForceSyncConfirmsPending 验证 target 确认 key 后 pending 队列归零。
func TestWriteBehindTargetForceSyncConfirmsPending(t *testing.T) {
	config := DefaultSharedConfig(t.TempDir())
	config.SyncWrites = true
	config.DetectConflicts = true
	config.CorruptionPolicy = CorruptionPolicyFail
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	target := newRecordingWriteBehindTarget[testFund]()
	t.Cleanup(target.releaseSync)
	require.NoError(t, db.UseWriteBehind(target))
	item := newFund("target-user", "spot", 1)
	require.NoError(t, db.Set(item, 0))
	require.Eventually(t, func() bool {
		return db.GetCachedPendingSyncCount() == 1
	}, writeBehindTargetEventuallyTimeout, writeBehindTargetEventuallyInterval)

	target.releaseSync()
	require.NoError(t, db.ForceSyncAll())
	require.Zero(t, db.GetCachedPendingSyncCount())
	calls, keys := target.snapshot()
	require.Positive(t, calls)
	require.Contains(t, keys, "testFund:target-user:spot")
}

// TestWriteBehindTargetTakesPrecedenceOverModelList 验证 target 和旧 syncList 同时存在时优先使用 target。
func TestWriteBehindTargetTakesPrecedenceOverModelList(t *testing.T) {
	config := DefaultSharedConfig(t.TempDir())
	config.SyncWrites = true
	config.DetectConflicts = true
	config.CorruptionPolicy = CorruptionPolicyFail
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	target := newRecordingWriteBehindTarget[testFund]()
	require.NoError(t, db.UseWriteBehind(target))
	action := newMemoryAction()
	db.syncLock.Lock()
	db.syncList = entity.NewModelList[testFund](action)
	db.syncLock.Unlock()

	require.NoError(t, db.Set(newFund("target-first", "spot", 1), 0))
	require.Eventually(t, target.startedSync, writeBehindTargetEventuallyTimeout, writeBehindTargetEventuallyInterval)
	target.releaseSync()
	require.NoError(t, db.ForceSyncAll())

	require.Zero(t, action.operationCallCount("insert"))
	calls, keys := target.snapshot()
	require.Positive(t, calls)
	require.Contains(t, keys, "testFund:target-first:spot")
}

// TestModelListWriteBehindTargetConfirmsInsertedItems 验证 ModelList 兼容 target 可写入 IDataAction 并确认 pending。
func TestModelListWriteBehindTargetConfirmsInsertedItems(t *testing.T) {
	config := DefaultSharedConfig(t.TempDir())
	config.SyncWrites = true
	config.DetectConflicts = true
	config.CorruptionPolicy = CorruptionPolicyFail
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	action := newMemoryAction()
	target := NewModelListWriteBehindTarget(entity.NewModelList[testFund](action))
	require.NoError(t, db.UseWriteBehind(target))

	require.NoError(t, db.Set(newFund("model-list-target", "spot", 1), 0))
	require.NoError(t, db.ForceSyncAll())
	require.Zero(t, db.GetCachedPendingSyncCount())
	require.Equal(t, 1, action.operationCallCount("insert"))
}

// TestUseWriteBehindRejectsSecondBinding 验证 write-behind 目标不能被静默替换。
func TestUseWriteBehindRejectsSecondBinding(t *testing.T) {
	config := DefaultSharedConfig(t.TempDir())
	config.SyncWrites = true
	config.CorruptionPolicy = CorruptionPolicyFail
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.UseWriteBehind(newRecordingWriteBehindTarget[testFund]()))
	err = db.UseWriteBehind(newRecordingWriteBehindTarget[testFund]())
	require.Error(t, err)
}

// TestWriteBehindTargetConfirmsSuccessfulKeysBeforeBatchError 验证部分失败时已成功 key 仍会 ACK。
func TestWriteBehindTargetConfirmsSuccessfulKeysBeforeBatchError(t *testing.T) {
	config := DefaultSharedConfig(t.TempDir())
	config.SyncWrites = true
	config.CorruptionPolicy = CorruptionPolicyFail
	config.AutoSync = false
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.UseWriteBehind(partialFailureWriteBehindTarget[testFund]{}))
	require.NoError(t, db.Set(newFund("partial-a", "spot", 1), 0))
	require.NoError(t, db.Set(newFund("partial-b", "spot", 2), 0))

	require.Error(t, db.ForceSyncAll())
	pending, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 1, pending)
}

// TestAutoSyncDisabledRequiresManualForceSync 验证关闭自动同步后写入不会被后台 worker 消费。
func TestAutoSyncDisabledRequiresManualForceSync(t *testing.T) {
	config := DefaultSharedConfig(t.TempDir())
	config.SyncWrites = true
	config.CorruptionPolicy = CorruptionPolicyFail
	config.AutoSync = false
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	target := newRecordingWriteBehindTarget[testFund]()
	require.NoError(t, db.UseWriteBehind(target))
	require.NoError(t, db.Set(newFund("manual-sync", "spot", 1), 0))
	require.Never(t, target.startedSync, 150*time.Millisecond, 10*time.Millisecond)

	target.releaseSync()
	require.NoError(t, db.ForceSyncAll())
	require.Zero(t, db.GetCachedPendingSyncCount())
}

// fundTargetFunc 为真实存储测试注入目标结果，不替代本地确认事务。
type fundTargetFunc func(context.Context, []*SyncQueueItem[testFund]) (*WriteBehindResult, error)

// SyncBatch 执行测试目标；确认数仍由真实Badger事务决定。
func (f fundTargetFunc) SyncBatch(ctx context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
	return f(ctx, items)
}

// TestWriteBehindPartialRoundCountsAndErrors 验证本地真实确认120条、原错误及剩余8条同时保留。
func TestWriteBehindPartialRoundCountsAndErrors(t *testing.T) {
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = false
	cfg.SyncBatchSize = 128
	db, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); _ = CloseSharedManager(path) })
	failure := errors.New("测试8条远端失败")
	require.NoError(t, db.UseWriteBehind(fundTargetFunc(func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		keys := []string{}
		for _, item := range items {
			if item.Item.Balance > 0 {
				keys = append(keys, item.Key)
			}
		}
		return &WriteBehindResult{ConfirmedKeys: keys}, failure
	})))
	for i := range 128 {
		value := float64(1)
		if i < 8 {
			value = 0
		}
		require.NoError(t, db.Set(newFund(fmt.Sprintf("partial-%03d", i), "spot", value), 0))
	}
	confirmed, err := db.processSyncQueue()
	require.Equal(t, 120, confirmed)
	require.ErrorIs(t, err, failure)
	metrics := db.GetSyncMetrics()
	require.Equal(t, uint64(1), metrics.Attempts)
	require.Equal(t, uint64(1), metrics.Failures)
	require.Equal(t, uint64(120), metrics.SyncedItems)
	require.True(t, metrics.LastSuccessAt.IsZero())
	pending, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 8, pending)
	require.Equal(t, 8, db.GetCachedPendingSyncCount())
	result, err := db.ForceSyncBatch(context.Background(), 128)
	require.ErrorIs(t, err, failure)
	require.Zero(t, result.Confirmed)
	require.Equal(t, 8, result.Remaining, "手动ForceSync仍返回原错误，不吞错绕过调度")
	require.Equal(t, uint64(120), db.GetSyncMetrics().SyncedItems)
}

// TestWriteBehindProgressUsesLocalConfirmation 验证重复返回、并发新版本和本地事务失败均不能伪造确认进展。
func TestWriteBehindProgressUsesLocalConfirmation(t *testing.T) {
	for _, mode := range []string{"duplicate", "new_version", "local_error"} {
		t.Run(mode, func(t *testing.T) {
			path := t.TempDir()
			cfg := DefaultProductionConfig(path)
			cfg.AutoSync = false
			db, err := NewSharedBadgerDB[testFund](path, cfg)
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close(); _ = CloseSharedManager(path) })
			failure := errors.New("测试远端返回的原始错误")
			require.NoError(t, db.UseWriteBehind(fundTargetFunc(func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
				if mode == "new_version" {
					require.NoError(t, db.Set(newFund("local-check", "spot", 2), 0))
				}
				if mode == "local_error" {
					require.NoError(t, CloseSharedManager(path))
				}
				keys := []string{items[0].Key, items[0].Key, items[0].Key}
				return &WriteBehindResult{ConfirmedKeys: keys}, failure
			})))
			item := newFund("local-check", "spot", 1)
			require.NoError(t, db.Set(item, 0))
			confirmed, err := db.processSyncQueue()
			require.ErrorIs(t, err, failure)
			metrics := db.GetSyncMetrics()
			require.Equal(t, uint64(1), metrics.Attempts)
			require.Equal(t, uint64(1), metrics.Failures)
			require.True(t, metrics.LastSuccessAt.IsZero())
			if mode == "duplicate" {
				require.Equal(t, 1, confirmed, "target返回3个重复key，本地只确认一次")
				require.Equal(t, uint64(1), metrics.SyncedItems)
				require.Zero(t, db.GetCachedPendingSyncCount())
				// 原ACK重放不形成新进展，不再次增加SyncedItems。
				w, err := db.getWrapper(db.generateKey(item))
				require.NoError(t, err)
				keys, err := db.syncBatchWithTargetContext(context.Background(), []*SyncQueueItem[testFund]{w}, fundTargetFunc(func(context.Context, []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
					return &WriteBehindResult{ConfirmedKeys: []string{w.Key, w.Key}}, nil
				}))
				require.NoError(t, err)
				require.Empty(t, keys)
				db.recordSyncMetrics(len(keys), err, 0)
				require.Equal(t, uint64(1), db.GetSyncMetrics().SyncedItems)
			} else {
				require.Zero(t, confirmed, "只有本地事务真正确认才算进展")
				require.Zero(t, metrics.SyncedItems)
				require.Equal(t, 1, db.GetCachedPendingSyncCount())
				schedule := adaptiveSyncSchedule{config: cfg}
				now := time.Now()
				schedule.completed(now, now, confirmed, err)
				require.Equal(t, cfg.SyncMinInterval, schedule.delay(now, 1, now))
				if mode == "local_error" {
					require.ErrorIs(t, err, badger.ErrDBClosed)
					require.Error(t, db.Close())
					require.NoError(t, CloseSharedManager(path))
					db, err = NewSharedBadgerDB[testFund](path, cfg)
					require.NoError(t, err)
				}
				exact, err := db.GetPendingSyncCount()
				require.NoError(t, err)
				require.Equal(t, 1, exact)
				value, err := db.getWrapper(db.generateKey(item))
				require.NoError(t, err)
				require.False(t, value.IsSynced)
			}
		})
	}
}
