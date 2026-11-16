// 本文件用真实可靠Badger与32个独立组固定确认排队和已确认组释放窗口，不用耗时SLO代替状态断言。
package nosql

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIndependentConfirmationConvoy32Groups 原FIFO阻塞时所有已退出target应能有界交入FIFO，不能由全局控制锁只放进一个。
func TestIndependentConfirmationConvoy32Groups(t *testing.T) {
	for _, auto := range []bool{false, true} {
		t.Run(fmt.Sprint("auto=", auto), func(t *testing.T) {
			entered, exited := make(chan struct{}, 32), make(chan struct{}, 32)
			firstRelease, secondRelease := make(chan struct{}), make(chan struct{})
			var onceFirst, onceSecond sync.Once
			releaseFirst := func() { onceFirst.Do(func() { close(firstRelease) }) }
			defer releaseFirst()
			releaseSecond := func() { onceSecond.Do(func() { close(secondRelease) }) }
			defer releaseSecond()
			target := &cleanupTestTarget{sync: func(_ context.Context, rows []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
				if rows[0].Item.Revision == 1 {
					entered <- struct{}{}
					<-firstRelease
					exited <- struct{}{}
				} else {
					<-secondRelease
				}
				return &WriteBehindResult{ConfirmedKeys: []string{rows[0].Key}}, nil
			}}
			s := openIndependentStore(t, t.TempDir(), auto, 32, target, func(c *BadgerDBConfig) { c.SyncBatchSize = 64; c.WriteBehindFailure.ScanLimit = 64 })
			rows := make([]*cleanupTestRecord, 0, 64)
			for revision := 1; revision <= 2; revision++ {
				for g := 0; g < 32; g++ {
					rows = append(rows, groupRecord(t, fmt.Sprintf("g%d-r%d", g, revision), fmt.Sprint(g), revision, false, &cleanupTestProbe{}))
				}
			}
			saved, e := s.SaveBatch(context.Background(), rows)
			require.NoError(t, e)
			require.Equal(t, 64, saved.Committed)
			firstDone := make(chan error, 1)
			if !auto {
				go func() {
					r, e := s.ForceSyncBatch(context.Background(), 64)
					if e == nil && r.Confirmed != 32 {
						e = fmt.Errorf("首轮真实确认=%d", r.Confirmed)
					}
					firstDone <- e
				}()
			}
			for range 32 {
				awaitGroupChannel(t, entered)
			}
			diskEntered, diskRelease := make(chan struct{}), make(chan struct{})
			var diskOnce sync.Once
			releaseDisk := func() { diskOnce.Do(func() { close(diskRelease) }) }
			defer releaseDisk()
			barrier, e := s.batcher.queueLocalCommit(context.Background(), func() error { close(diskEntered); <-diskRelease; return nil })
			require.NoError(t, e)
			awaitGroupChannel(t, diskEntered)
			releaseFirst()
			for range 32 {
				awaitGroupChannel(t, exited)
			}
			assert.Eventually(t, func() bool { return len(s.batcher.requests) >= 32 }, time.Second, time.Millisecond, "32个已退出target必须都能交入原FIFO，不可持全局控制锁等待提交")
			require.Equal(t, 64, s.db.GetCachedPendingSyncCount(), "FIFO尚未提交，不能提前确认")
			releaseDisk()
			require.NoError(t, (<-barrier).err)
			require.Eventually(t, func() bool { return s.Metrics().Sync.SyncedItems == 32 }, 3*time.Second, time.Millisecond)
			assert.LessOrEqual(t, s.Metrics().Sync.ConfirmationLocal.Committed, uint64(8), "相邻32项确认须沿原8项量子合批，而非32次逐项提交")
			t.Logf("32个实际target结果：确认事务=%d，单事务最大输入=%d，可靠pending=%d", s.Metrics().Sync.ConfirmationLocal.Committed, s.Metrics().Sync.ConfirmationLocal.MaxItems, s.Metrics().Pending)
			releaseSecond()
			if !auto {
				require.NoError(t, <-firstDone)
				r, e := s.ForceSyncAll(context.Background())
				require.NoError(t, e)
				require.Equal(t, 32, r.Confirmed)
			}
			require.Eventually(t, func() bool {
				m := s.Metrics()
				return m.Pending == 0 && m.Sync.SyncedItems == 64 && s.db.disposition.groups.active.Load() == 0
			}, 3*time.Second, time.Millisecond)
			records, _, e := s.ListWriteBehindFailures("", 128)
			require.NoError(t, e)
			require.Len(t, records, 64)
			for _, r := range records {
				require.Equal(t, 1, r.Attempts)
				require.Equal(t, WriteBehindCompleted, r.State)
			}
		})
	}
}

// TestIndependentConfirmationReleaseDoesNotReenterControlLock 已提交结果在指标边界暂停后，组/槽归还不能再等无关控制锁。
func TestIndependentConfirmationReleaseDoesNotReenterControlLock(t *testing.T) {
	committed, continueResult := make(chan struct{}), make(chan struct{})
	target := &cleanupTestTarget{}
	s := openIndependentStore(t, t.TempDir(), false, 2, target)
	s.db.disposition.confirmationObserver = func(rows []*SyncQueueItem[cleanupTestRecord]) {
		if len(rows) > 0 {
			close(committed)
			<-continueResult
		}
	}
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "one", "A", 1, false, &cleanupTestProbe{})))
	done := make(chan error, 1)
	go func() { _, e := s.ForceSyncBatch(context.Background(), 16); done <- e }()
	awaitGroupChannel(t, committed)
	// 真实确认事务已提交，固定worker尚未跨过原recordSyncMetrics；随后模拟无关选批占有控制锁。
	s.db.syncMetricsMu.Lock()
	close(continueResult)
	s.db.syncExecMu.Lock()
	var once sync.Once
	unlock := func() { once.Do(func() { s.db.syncExecMu.Unlock() }) }
	defer unlock()
	s.db.syncMetricsMu.Unlock()
	require.Eventually(t, func() bool {
		return s.db.disposition.groups.active.Load() == 0 && len(s.db.disposition.groups.slots) == 2
	}, time.Second, time.Millisecond, "已确认且回调退出的组/槽不应第二次等待控制锁")
	require.Zero(t, s.db.GetCachedPendingSyncCount())
	unlock()
	require.NoError(t, <-done)
}
