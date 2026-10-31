// 本文件只对显式指定的Core合成卷副本做跨版本读取/恢复，不连接消费方或原始测量目录。
package nosql

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestConfirmationVolumeCompatibility 由独立旧/新测试进程打开副本，核对同policy原卷及无新Save恢复。
func TestConfirmationVolumeCompatibility(t *testing.T) {
	base := os.Getenv("CORE_CONFIRM_COMPAT_COPY")
	if base == "" {
		t.Skip("仅显式Core合成卷副本跨版本验收")
	}
	marker, err := os.ReadFile(filepath.Join(base, ".core-confirmation-synthetic-copy"))
	require.NoError(t, err, "只允许显式Core合成副本，拒绝原卷/消费方路径")
	require.Equal(t, "core-confirmation-synthetic-copy", string(marker))
	auto := os.Getenv("CORE_CONFIRM_COMPAT_DRAIN") == "1"
	cfg := DefaultProductionConfig(base)
	cfg.EnableLogger = false
	cfg.AutoSync = auto
	cfg.SyncBatchSize = 128
	cfg.SyncFlushThreshold = 32
	cfg.SyncMaxCollectDelay = 20 * time.Millisecond
	cfg.SyncBacklogDrainDelay = 0
	cfg.WriteBehindFailure = &WriteBehindFailurePolicy{StoreID: "core-confirm-performance", MaxAttempts: 3, MaxElapsed: 5 * time.Minute, MinRetryInterval: time.Second, MaxRetryInterval: 10 * time.Second, HandlerMaxAttempts: 8, HandlerTimeout: 2 * time.Second, HandlerWorkers: 2, ScanLimit: 128}
	store, _, err := NewReliableWriteStore[confirmationLoadRecord](ServiceIdentity{ServiceName: "core-confirm-performance"}, ReliableWriteStoreConfig{BasePath: base, Badger: cfg, Batch: BatchCommitConfig{MaxBatch: 128, CollectBacklog: 1, CollectWindow: time.Millisecond, QueueCapacity: 1024}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close(context.Background()); _ = CloseSharedManager(store.config.BasePath) })
	initial, err := store.db.GetPendingSyncCount()
	require.NoError(t, err)
	if auto {
		require.Equal(t, 2000, initial)
	} else {
		require.Zero(t, initial)
	}
	a := &confirmationLoadTarget{}
	require.NoError(t, store.UseWriteBehindWithFailureHandler(a, a))
	require.Eventually(t, func() bool { return store.Metrics().Pending == 0 }, 15*time.Second, time.Millisecond)
	items, err := store.ScanLocal(context.Background(), LocalScanOptions{Limit: 3000})
	require.NoError(t, err)
	require.Len(t, items, 2000)
	for _, item := range items {
		require.Equal(t, 2028, len(item.Payload))
	}
	if auto {
		require.Eventually(t, func() bool { return store.Metrics().Sync.SyncedItems == 2000 }, time.Second, time.Millisecond)
	} else {
		require.Zero(t, a.calls.Load(), "旧代码初始化并核对已完成新卷，不能重复target")
	}
	count := 0
	after := ""
	for {
		records, next, e := store.ListWriteBehindFailures(after, 1000)
		require.NoError(t, e)
		for _, r := range records {
			require.Equal(t, WriteBehindCompleted, r.State)
			require.True(t, r.LocalConfirmed)
			require.Equal(t, 1, r.Attempts)
		}
		count += len(records)
		if next == "" {
			break
		}
		after = next
	}
	require.Equal(t, 2000, count)
	require.NoError(t, store.Close(context.Background()))
	require.NoError(t, CloseSharedManager(store.config.BasePath))
	t.Logf("跨版本独立进程原卷副本：auto=%t，2000原件/完成历史逐项核对，无新Save", auto)
}

// TestDispositionGroupVolumeCompatibility 用三个独立进程验证旧算法建卷、新算法改组排空、旧算法读取完成历史。
func TestDispositionGroupVolumeCompatibility(t *testing.T) {
	base := os.Getenv("CORE_GROUP_COMPAT_COPY")
	if base == "" {
		t.Skip("仅显式Core空组核验合成副本兼容验收")
	}
	marker, err := os.ReadFile(filepath.Join(base, ".core-group-synthetic-copy"))
	require.NoError(t, err)
	require.Equal(t, "core-group-synthetic-copy", string(marker))
	role := os.Getenv("CORE_GROUP_COMPAT_ROLE")
	require.Contains(t, []string{"seed", "update", "verify"}, role)
	store := groupOperationsStore(t, base, false)
	if role == "seed" {
		items := make([]*confirmationLoadRecord, 32)
		for i := range items {
			items[i] = groupOperationRecord(fmt.Sprint(i), 1, 2028)
		}
		_, err := store.SaveBatch(context.Background(), items)
		require.NoError(t, err)
		for i := range 16 {
			items[i] = groupOperationRecord(fmt.Sprint(i), 2, 2029)
		}
		_, err = store.SaveBatch(context.Background(), items[:16])
		require.NoError(t, err)
		require.Equal(t, 32, store.Metrics().Pending)
	} else if role == "update" {
		require.Equal(t, 32, store.Metrics().Pending, "无新请求前已经恢复原pending")
		requireDispositionGroup(t, store.db, 1, true)
		requireDispositionGroup(t, store.db, 2, true)
		items := make([]*confirmationLoadRecord, 32)
		for i := range items {
			items[i] = groupOperationRecord(fmt.Sprint(i), 3, 2030)
		}
		_, err := store.SaveBatch(context.Background(), items)
		require.NoError(t, err)
		requireDispositionGroup(t, store.db, 1, false)
		requireDispositionGroup(t, store.db, 2, false)
		_, err = store.ForceSyncAll(context.Background())
		require.NoError(t, err)
		require.Zero(t, store.Metrics().Pending)
		requireDispositionGroup(t, store.db, 3, false)
	} else {
		require.Zero(t, store.Metrics().Pending)
		result, err := store.ForceSyncAll(context.Background())
		require.NoError(t, err)
		require.Zero(t, result.Confirmed, "已完成记录不重投")
		for i := range 32 {
			r := currentGroupRecord(t, store, fmt.Sprint(i))
			require.Equal(t, WriteBehindCompleted, r.State)
			require.True(t, r.LocalConfirmed)
			require.Equal(t, "3", r.Group)
			require.Len(t, r.Snapshot.Item.Payload, 2030)
		}
		records, next, err := store.ListWriteBehindFailures("", 100)
		require.NoError(t, err)
		require.Empty(t, next)
		require.Len(t, records, 80, "原始、Superseded和完成历史都保留")
	}
}
