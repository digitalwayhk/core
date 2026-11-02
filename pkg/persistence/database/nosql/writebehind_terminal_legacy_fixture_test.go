// 本文件只使用v1.8.1已有API，供精确旧源码独立进程建立真实存量卷及验证旧二进制拒绝新格式。
package nosql

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func terminalLegacyFixtureStore(t *testing.T, base string) *ReliableWriteStore[cleanupTestRecord] {
	t.Helper()
	cfg := DefaultProductionConfig(base)
	cfg.AutoSync = false
	cfg.EnableLogger = false
	cfg.SyncBatchSize = 16
	cfg.WriteBehindFailure = &WriteBehindFailurePolicy{StoreID: "terminal-fixture", MaxAttempts: 3, MinRetryInterval: time.Millisecond, MaxRetryInterval: 10 * time.Millisecond, HandlerMaxAttempts: 2, HandlerTimeout: time.Second, HandlerWorkers: 1, ScanLimit: 16}
	s, _, e := NewReliableWriteStore[cleanupTestRecord](ServiceIdentity{ServiceName: "terminal-fixture", DataCenterID: 1, MachineID: 1}, ReliableWriteStoreConfig{BasePath: base, Badger: cfg, CloseTimeout: time.Second})
	require.NoError(t, e)
	t.Cleanup(func() { _ = s.Close(context.Background()); _ = CloseSharedManager(s.config.BasePath) })
	return s
}
func terminalLegacyFixtureTarget() *cleanupTestTarget {
	return &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		r := &WriteBehindResult{}
		var e error
		for _, item := range items {
			if item.Item.Key == "paused" {
				e = errors.New("测试未知结果")
			} else {
				r.ConfirmedKeys = append(r.ConfirmedKeys, item.Key)
			}
		}
		return r, e
	}}
}

// TestTerminalLegacyVolumeFixture仅在Core合成目录建立真实旧格式快照；reject角色证明旧代码在绑定阶段失败关闭。
func TestTerminalLegacyVolumeFixture(t *testing.T) {
	role := os.Getenv("CORE_TERMINAL_LEGACY_ROLE")
	if role == "" {
		t.Skip("独立旧版本进程夹具")
	}
	s := terminalLegacyFixtureStore(t, os.Getenv("CORE_TERMINAL_ROOT"))
	target := terminalLegacyFixtureTarget()
	e := s.UseWriteBehindWithFailureHandler(target, target)
	if role == "reject" {
		require.ErrorIs(t, e, ErrWriteBehindDisposition)
		return
	}
	require.Equal(t, "seed", role)
	require.NoError(t, e)
	for i := 0; i < 32; i++ {
		for v := 0; v < 3; v++ {
			require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, fmt.Sprint(i), v, false, &cleanupTestProbe{})))
		}
	}
	_, e = s.ForceSyncAll(context.Background())
	require.NoError(t, e)
	require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, "paused", 1, false, &cleanupTestProbe{})))
	_, e = s.ForceSyncBatch(context.Background(), 16)
	require.Error(t, e)
	require.Eventually(t, func() bool { m, e := s.WriteBehindDispositionMetrics(); return e == nil && m.Paused == 1 }, time.Second, time.Millisecond)
	later := newCleanupTestRecord(t, "successor", 1, false, &cleanupTestProbe{})
	later.Group = "paused"
	require.NoError(t, s.Save(context.Background(), later))
	require.Equal(t, 2, s.Metrics().Pending)
	r, _, e := s.ListWriteBehindFailures("", 1000)
	require.NoError(t, e)
	require.Len(t, r, 98)
	for _, r := range r {
		require.NotNil(t, r.Snapshot.Item, "旧版本必须真实保留完整快照")
	}
}
