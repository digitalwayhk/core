// 本文件以真实独立PID硬退出后重开原临时Badger，验证重启不伪造内存原错误证明。
package nosql

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDeferredCauseProcess 只由父测试启动；seed在原回调未退出时硬杀，recover不新Save且不重做target。
func TestDeferredCauseProcess(t *testing.T) {
	role := os.Getenv("CORE_TEST_DEFERRED_CAUSE_ROLE")
	if role == "" {
		t.Skip("仅供独立进程父测试启动")
	}
	require.Contains(t, []string{"seed", "recover"}, role)
	root := os.Getenv("CORE_TEST_DEFERRED_CAUSE_ROOT")
	base := filepath.Join(root, "volume")
	cfg := DefaultProductionConfig(base)
	cfg.AutoSync = role == "recover"
	cfg.EnableLogger = false
	cfg.SyncBatchSize = 32
	cfg.WriteBehindFailure = &WriteBehindFailurePolicy{StoreID: "cause-process", MaxAttempts: 3, MaxElapsed: time.Minute, MinRetryInterval: time.Millisecond, MaxRetryInterval: 20 * time.Millisecond, HandlerMaxAttempts: 3, HandlerTimeout: 20 * time.Second, HandlerWorkers: 2, ScanLimit: 64}
	var targetCalls atomic.Int32
	entered := make(chan struct{}, 2)
	observed := make(chan bool, 32)
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		targetCalls.Add(1)
		return nil, deferredProof(items)
	}}
	target.handle = func(_ context.Context, f WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error) {
		if role == "seed" {
			entered <- struct{}{}
			select {}
		}
		var proof *deferredCauseProof
		observed <- errors.As(f.Cause, &proof)
		return cleanupTestDecision(f.Record, WriteBehindPause), nil
	}
	store, _, err := NewReliableWriteStore[cleanupTestRecord](ServiceIdentity{ServiceName: "cause-process"}, ReliableWriteStoreConfig{BasePath: base, Badger: cfg})
	require.NoError(t, err)
	require.NoError(t, store.UseWriteBehindWithFailureHandler(target, target))
	if role == "seed" {
		for i := range 2 {
			require.NoError(t, store.Save(context.Background(), newCleanupTestRecord(t, string(rune('a'+i)), 1, false, &cleanupTestProbe{})))
		}
		_, err = store.ForceSyncBatch(context.Background(), 32)
		require.Error(t, err)
		for range 2 {
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("seed handler未进入")
			}
		}
		var items []*cleanupTestRecord
		for i := 2; i < 32; i++ {
			items = append(items, newCleanupTestRecord(t, string(rune('a'+i)), 2, false, &cleanupTestProbe{}))
		}
		result, e := store.SaveBatch(context.Background(), items)
		require.NoError(t, e)
		require.Equal(t, 30, result.Committed)
		_, err = store.ForceSyncBatch(context.Background(), 32)
		require.Error(t, err)
		m, e := store.WriteBehindDispositionMetrics()
		require.NoError(t, e)
		require.Equal(t, 28, m.CauseRetained)
		require.Equal(t, 32, store.Metrics().Pending)
		require.NoError(t, os.WriteFile(filepath.Join(root, "ready"), []byte("32原件/28内存错误引用，原handler尚未退出"), 0600))
		select {}
	}
	require.Eventually(t, func() bool { m, e := store.WriteBehindDispositionMetrics(); return e == nil && m.Paused == 32 }, 5*time.Second, time.Millisecond)
	for range 32 {
		select {
		case typed := <-observed:
			require.False(t, typed, "新PID不能从字符串伪造原类型")
		case <-time.After(time.Second):
			t.Fatal("重启回调观察量不足")
		}
	}
	require.Zero(t, targetCalls.Load(), "未知结果不得盲重做target")
	require.Equal(t, 32, store.Metrics().Pending)
	records, _, err := store.ListWriteBehindFailures("", 64)
	require.NoError(t, err)
	require.Len(t, records, 32)
	counts := map[int]int{}
	for _, r := range records {
		require.Equal(t, 1, r.Attempts)
		counts[r.HandlerAttempts]++
	}
	require.Equal(t, 4, counts[2])
	require.Equal(t, 28, counts[1])
	var pending *PendingSyncError
	require.ErrorAs(t, store.Close(context.Background()), &pending)
	require.Equal(t, 32, pending.Count)
	require.NoError(t, CloseSharedManager(store.config.BasePath))
}

// TestDeferredCauseHardCrashDropsOnlyMemoryProof 验证真实硬杀与新PID重开，原件/预算保留但内存证明不跨进程。
func TestDeferredCauseHardCrashDropsOnlyMemoryProof(t *testing.T) {
	root := t.TempDir()
	exe, err := os.Executable()
	require.NoError(t, err)
	start := func(role string) (*exec.Cmd, *os.File) {
		f, e := os.Create(filepath.Join(root, role+".log"))
		require.NoError(t, e)
		cmd := exec.Command(exe, "-test.run=^TestDeferredCauseProcess$", "-test.v", "-test.timeout=15s")
		cmd.Env = append(os.Environ(), "CORE_TEST_DEFERRED_CAUSE_ROLE="+role, "CORE_TEST_DEFERRED_CAUSE_ROOT="+root)
		cmd.Stdout = f
		cmd.Stderr = f
		require.NoError(t, cmd.Start())
		return cmd, f
	}
	seed, seedLog := start("seed")
	t.Cleanup(func() { _ = seed.Process.Kill(); _ = seedLog.Close() })
	require.Eventually(t, func() bool { _, e := os.Stat(filepath.Join(root, "ready")); return e == nil }, 5*time.Second, time.Millisecond)
	require.NoError(t, seed.Process.Kill())
	require.Error(t, seed.Wait())
	require.NoError(t, seedLog.Close())
	recover, recoverLog := start("recover")
	err = recover.Wait()
	require.NoError(t, recoverLog.Close())
	if err != nil {
		b, _ := os.ReadFile(filepath.Join(root, "recover.log"))
		t.Log(string(b))
	}
	require.NoError(t, err)
	t.Logf("seed_pid=%d recover_pid=%d；硬杀后原pending32/attempt1保持，无target重放、无typed证明", seed.Process.Pid, recover.Process.Pid)
}
