// 本文件复用现有Core子进程/双Badger方式，验证普通target确认窗口硬杀后由新PID恢复原卷。
package nosql

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

// TestPlainConfirmationCrashProcess 是受环境变量保护的真实子进程入口，普通全包不自行启动。
func TestPlainConfirmationCrashProcess(t *testing.T) {
	mode := os.Getenv("CORE_PLAIN_CONFIRM_MODE")
	if mode == "" {
		t.Skip("由普通确认硬杀父测试启动")
	}
	stage, path := os.Getenv("CORE_PLAIN_CONFIRM_STAGE"), os.Getenv("CORE_PLAIN_CONFIRM_ROOT")
	ready := func() {
		require.NoError(t, os.WriteFile(filepath.Join(path, "crash.ready"), []byte(stage), 0600))
		select {}
	}
	app, err := badger.Open(badger.DefaultOptions(filepath.Join(path, "app")).WithLogger(nil).WithSyncWrites(true))
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Close() })
	target := &cleanupTestTarget{}
	target.sync = func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		// 应用持久事实幂等；普通at-least-once恢复可以再次调用target，不能重复应用效果。
		require.NoError(t, app.Update(func(txn *badger.Txn) error {
			count := 0
			e := dispositionRead(txn, "effect", &count)
			if e != nil && e != badger.ErrKeyNotFound {
				return e
			}
			if count == 0 {
				return dispositionWrite(txn, "effect", 1)
			}
			return nil
		}))
		if mode == "crash" && stage == "before_confirmation" {
			ready()
		}
		result := &WriteBehindResult{}
		for _, row := range items {
			result.ConfirmedKeys = append(result.ConfirmedKeys, row.Key)
		}
		return result, nil
	}
	store := openCleanupTestStore(t, filepath.Join(path, "core"), mode == "recover", false, target)
	if mode == "crash" {
		probe := &cleanupTestProbe{}
		if stage == "after_confirmation" {
			probe.hook = func(*cleanupTestRecord) { ready() }
		}
		if stage == "save_unknown" {
			original := store.batcher.commit
			store.batcher.commit = func(ops []WriteOperation[cleanupTestRecord]) (BatchWriteResult, error) {
				r, e := original(ops)
				require.NoError(t, e)
				require.Equal(t, 1, r.Committed)
				ready()
				return r, e
			}
		}
		require.NoError(t, store.Save(context.Background(), newCleanupTestRecord(t, "original", 1, true, probe)))
		_, err = store.ForceSyncBatch(context.Background(), 16)
		require.NoError(t, err)
		select {}
	}
	if stage != "after_confirmation" {
		// 启动初始缓存可能尚为0；必须观察到本进程真实确认，不把尚未启动当排空。
		require.Eventually(t, func() bool { m := store.Metrics(); return m.Pending == 0 && m.Sync.SyncedItems == 1 }, 5*time.Second, time.Millisecond)
	}
	count := 0
	require.NoError(t, app.View(func(txn *badger.Txn) error { return dispositionRead(txn, "effect", &count) }))
	require.Equal(t, 1, count)
	if stage == "after_confirmation" {
		row, e := store.GetLocal(context.Background(), "original")
		require.NoError(t, e)
		require.NotNil(t, row)
		require.True(t, row.DeleteAfter, "确认后尚未物理回收，重启不伪造补删")
	}
	require.NoError(t, store.Close(context.Background()))
	require.NoError(t, CloseSharedManager(store.config.BasePath))
}

// TestPlainConfirmationHardCrashRecovery 验证目标成功前后和Save结果未知，原消息/原卷由新PID收敛且无新Save。
func TestPlainConfirmationHardCrashRecovery(t *testing.T) {
	for _, stage := range []string{"before_confirmation", "after_confirmation", "save_unknown"} {
		t.Run(stage, func(t *testing.T) {
			path := t.TempDir()
			exe, err := os.Executable()
			require.NoError(t, err)
			start := func(mode string) (*exec.Cmd, *os.File) {
				log, err := os.Create(filepath.Join(path, mode+".log"))
				require.NoError(t, err)
				cmd := exec.Command(exe, "-test.run=^TestPlainConfirmationCrashProcess$", "-test.timeout=20s")
				cmd.Env = append(os.Environ(), "CORE_PLAIN_CONFIRM_MODE="+mode, "CORE_PLAIN_CONFIRM_STAGE="+stage, "CORE_PLAIN_CONFIRM_ROOT="+path)
				cmd.Stdout = log
				cmd.Stderr = log
				require.NoError(t, cmd.Start())
				t.Cleanup(func() {
					_ = cmd.Process.Kill()
					_ = log.Close()
					if t.Failed() {
						data, _ := os.ReadFile(log.Name())
						t.Logf("%s PID=%d %s", mode, cmd.Process.Pid, data)
					}
				})
				return cmd, log
			}
			crash, log := start("crash")
			require.Eventually(t, func() bool { _, e := os.Stat(filepath.Join(path, "crash.ready")); return e == nil }, 10*time.Second, time.Millisecond)
			require.NoError(t, crash.Process.Kill())
			require.Error(t, crash.Wait())
			require.NoError(t, log.Close())
			recover, log := start("recover")
			require.NotEqual(t, crash.Process.Pid, recover.Process.Pid)
			require.NoError(t, recover.Wait())
			require.NoError(t, log.Close())
			t.Logf("普通确认真实硬杀PID=%d，新PID=%d，stage=%s，无新Save打开原卷恢复", crash.Process.Pid, recover.Process.Pid, stage)
		})
	}
}
