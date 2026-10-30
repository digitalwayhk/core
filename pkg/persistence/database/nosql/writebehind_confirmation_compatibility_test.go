// 本文件只对显式指定的Core合成卷副本做跨版本读取/恢复，不连接消费方或原始测量目录。
package nosql

import (
	"context"
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
