// 本文件沿现有独立组进程入口验证32组target已提交、原确认前/后硬杀，新PID不发新Save恢复。
package nosql

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

func runIndependentConfirmationProcess(t *testing.T, mode, root, stage string, app *badger.DB) {
	ready := func() {
		require.NoError(t, os.WriteFile(filepath.Join(root, "crash.ready"), []byte(stage), 0600))
		select {}
	}
	entered, exited, release := make(chan struct{}, 32), make(chan struct{}, 32), make(chan struct{})
	target := &cleanupTestTarget{}
	target.sync = func(_ context.Context, rows []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		key := rows[0].Item.Key
		count, e := processAppCount(app, key, false)
		if e != nil {
			return nil, e
		}
		if count == 0 {
			_, e = processAppCount(app, key, true)
			if e != nil {
				return nil, e
			}
		}
		if mode == "crash" {
			entered <- struct{}{}
			<-release
			exited <- struct{}{}
		}
		return &WriteBehindResult{ConfirmedKeys: []string{rows[0].Key}}, nil
	}
	target.handle = func(_ context.Context, f WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error) {
		count, e := processAppCount(app, f.Record.Snapshot.Item.Key, false)
		if e != nil {
			return WriteBehindDecision{}, e
		}
		require.Equal(t, 1, count, "先读原应用事实，不盲重调target")
		d := cleanupTestDecision(f.Record, WriteBehindConfirm)
		d.Outcome = WriteBehindCommitted
		return d, nil
	}
	s := openIndependentStore(t, filepath.Join(root, "core"), mode == "recover", 32, target, func(c *BadgerDBConfig) { c.SyncBatchSize = 64; c.WriteBehindFailure.ScanLimit = 64 })
	if mode == "crash" {
		if stage == "after_confirmation" {
			var once sync.Once
			s.db.disposition.confirmationObserver = func(rows []*SyncQueueItem[cleanupTestRecord]) {
				if len(rows) > 0 {
					once.Do(ready)
				}
			}
		}
		rows := make([]*cleanupTestRecord, 32)
		for i := range rows {
			rows[i] = groupRecord(t, fmt.Sprint(i), fmt.Sprint(i), 1, false, &cleanupTestProbe{})
		}
		saved, e := s.SaveBatch(context.Background(), rows)
		require.NoError(t, e)
		require.Equal(t, 32, saved.Committed)
		go func() { _, _ = s.ForceSyncBatch(context.Background(), 64) }()
		for range 32 {
			awaitGroupChannel(t, entered)
		}
		if stage == "before_confirmation" {
			diskEntered := make(chan struct{})
			_, e := s.batcher.queueLocalCommit(context.Background(), func() error { close(diskEntered); ready(); return nil })
			require.NoError(t, e)
			awaitGroupChannel(t, diskEntered)
			close(release)
			for range 32 {
				awaitGroupChannel(t, exited)
			}
			require.Eventually(t, func() bool { return len(s.batcher.requests) >= 32 }, time.Second, time.Millisecond)
			ready()
		}
		close(release)
		select {}
	}
	// 每个原pending仍在，恢复不靠新订单；原应用事实与最终32个持久终态都必须存在。
	require.Eventually(t, func() bool {
		n, e := s.db.GetPendingSyncCount()
		if e != nil || n != 0 {
			return false
		}
		r, _, e := s.ListWriteBehindFailures("", 64)
		if e != nil || len(r) != 32 {
			return false
		}
		for _, v := range r {
			if v.State != WriteBehindCompleted || v.Attempts != 1 {
				return false
			}
		}
		return true
	}, 5*time.Second, time.Millisecond)
	for i := 0; i < 32; i++ {
		n, e := processAppCount(app, fmt.Sprint(i), false)
		require.NoError(t, e)
		require.Equal(t, 1, n)
	}
	require.NoError(t, s.Close(context.Background()))
	require.NoError(t, CloseSharedManager(s.config.BasePath))
	require.NoError(t, os.WriteFile(filepath.Join(root, "recover.ready"), []byte(stage+"-original-32-facts-no-new-save"), 0600))
}
