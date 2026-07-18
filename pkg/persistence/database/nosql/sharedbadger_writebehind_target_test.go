// Package nosql 验证 PrefixedBadgerDB 可插拔 WriteBehindTarget 的可靠 ACK 行为。
package nosql

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
)

const (
	writeBehindTargetEventuallyTimeout  = 3 * time.Second
	writeBehindTargetEventuallyInterval = 20 * time.Millisecond
)

type recordingWriteBehindTarget[T types.IModel] struct {
	mu        sync.Mutex
	keys      []string
	calls     int
	started   atomic.Bool
	release   chan struct{}
	blockOnce sync.Once
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
	close(tgt.release)
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
