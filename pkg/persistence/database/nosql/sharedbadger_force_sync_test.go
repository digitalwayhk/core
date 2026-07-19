// Package nosql 验证 WriteBehind 有界同步的 limit、部分确认、context 和无进展边界。
package nosql

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type boundedFundTarget struct {
	mu             sync.Mutex
	confirm        int
	err            error
	waitForContext bool
	calls          int
	received       []int
}

func (target *boundedFundTarget) SyncBatch(ctx context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
	target.mu.Lock()
	target.calls++
	target.received = append(target.received, len(items))
	waitForContext := target.waitForContext
	confirm := target.confirm
	targetErr := target.err
	target.mu.Unlock()
	if waitForContext {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if confirm > len(items) {
		confirm = len(items)
	}
	keys := make([]string, 0, confirm)
	for _, item := range items[:confirm] {
		keys = append(keys, item.Key)
	}
	return &WriteBehindResult{ConfirmedKeys: keys}, targetErr
}

func (target *boundedFundTarget) snapshot() (int, []int) {
	target.mu.Lock()
	defer target.mu.Unlock()
	return target.calls, append([]int(nil), target.received...)
}

func newBoundedSyncFundDB(t *testing.T, target *boundedFundTarget, count int) *PrefixedBadgerDB[testFund] {
	t.Helper()
	path := t.TempDir()
	config := DefaultProductionConfig(path)
	config.AutoSync = false
	db, err := NewSharedBadgerDB[testFund](path, config)
	require.NoError(t, err)
	require.NoError(t, db.UseWriteBehind(target))
	for index := range count {
		require.NoError(t, db.Set(newFund(fmt.Sprintf("bounded-%d", index), "HK", 1), 0))
	}
	t.Cleanup(func() {
		_ = db.Close()
		_ = CloseSharedManager(path)
	})
	return db
}

func TestForceSyncBatchHonorsLimitAndPartialConfirmation(t *testing.T) {
	remoteErr := errors.New("remote partial failure")
	target := &boundedFundTarget{confirm: 2, err: remoteErr}
	db := newBoundedSyncFundDB(t, target, 5)

	result, err := db.ForceSyncBatch(context.Background(), 3)
	require.ErrorIs(t, err, remoteErr)
	require.Equal(t, ForceSyncResult{Confirmed: 2, Remaining: 3}, result)
	calls, received := target.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, []int{3}, received)
	require.Equal(t, 3, db.GetCachedPendingSyncCount())
}

func TestForceSyncBatchRejectsInvalidLimit(t *testing.T) {
	target := &boundedFundTarget{}
	db := newBoundedSyncFundDB(t, target, 1)

	_, err := db.ForceSyncBatch(context.Background(), 0)
	require.ErrorIs(t, err, ErrInvalidSyncLimit)
	calls, _ := target.snapshot()
	require.Zero(t, calls)
}

func TestForceSyncBatchDoesNotCallTargetAfterCancellation(t *testing.T) {
	target := &boundedFundTarget{confirm: 1}
	db := newBoundedSyncFundDB(t, target, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := db.ForceSyncBatch(ctx, 1)
	require.ErrorIs(t, err, context.Canceled)
	calls, _ := target.snapshot()
	require.Zero(t, calls)
}

func TestForceSyncBatchPassesContextToTarget(t *testing.T) {
	target := &boundedFundTarget{waitForContext: true}
	db := newBoundedSyncFundDB(t, target, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := db.ForceSyncBatch(ctx, 1)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	calls, _ := target.snapshot()
	require.Equal(t, 1, calls)
}

func TestForceSyncAllContextStopsWithoutProgress(t *testing.T) {
	target := &boundedFundTarget{confirm: 0}
	db := newBoundedSyncFundDB(t, target, 2)

	result, err := db.ForceSyncAllContext(context.Background())
	require.ErrorIs(t, err, ErrWriteBehindNoProgress)
	require.Equal(t, ForceSyncResult{Confirmed: 0, Remaining: 2}, result)
	calls, _ := target.snapshot()
	require.Equal(t, 1, calls)
}
