// 本文件用真实在途target证明选批不重复读取不可能准入的组；不改变handler期限或组首事务。
package nosql

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

// TestIndependentGroupsSkipActiveTargetScan 保留16个实际在途组，健康新组仍可选中且不重取在途原件。
func TestIndependentGroupsSkipActiveTargetScan(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	entered := make(chan struct{}, 16)
	target := &cleanupTestTarget{sync: func(_ context.Context, rows []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		if rows[0].Item.Group != "healthy" {
			entered <- struct{}{}
			<-release
		}
		return &WriteBehindResult{ConfirmedKeys: []string{rows[0].Key}}, nil
	}}
	s := openIndependentStore(t, t.TempDir(), false, 32, target, func(c *BadgerDBConfig) { c.SyncBatchSize = 128; c.WriteBehindFailure.ScanLimit = 128 })
	defer unblock()
	var lookups atomic.Int32
	s.db.disposition.selectionObserver = func(kind string, _ badger.IteratorOptions, _, _ time.Duration) {
		if kind == "queue" {
			lookups.Add(1)
		}
	}
	for i := range 16 {
		require.NoError(t, s.Save(context.Background(), groupRecord(t, fmt.Sprint("held-", i), fmt.Sprint("group-", i), 1, false, &cleanupTestProbe{})))
	}
	jobs, _, err := s.db.enqueueIndependentGroups(context.Background(), 128)
	require.NoError(t, err)
	require.Len(t, jobs, 16)
	for range 16 {
		awaitGroupChannel(t, entered)
	}
	lookups.Store(0)
	for range 20 {
		jobs, _, err = s.db.enqueueIndependentGroups(context.Background(), 128)
		require.NoError(t, err)
		require.Empty(t, jobs)
	}
	require.Zero(t, lookups.Load(), "已知target在途且未归还原槽的组，不应再queue Seek/解码全原件")
	require.Equal(t, 16, s.Metrics().Pending)
	require.NoError(t, s.Save(context.Background(), groupRecord(t, "new", "healthy", 1, false, &cleanupTestProbe{})))
	jobs, _, err = s.db.enqueueIndependentGroups(context.Background(), 128)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Eventually(t, func() bool { return s.Metrics().Pending == 16 }, time.Second, time.Millisecond)
	require.EqualValues(t, 1, lookups.Load(), "新健康组读取其真实组首，在途组仍不重复读取")
	unblock()
	require.Eventually(t, func() bool { return s.Metrics().Pending == 0 }, time.Second, time.Millisecond)
}
