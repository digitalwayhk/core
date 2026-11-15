// 本文件用独立PID和仅验收期依赖断点副本固定Truncate(0)到Remove之间的真实硬杀窗口。
package nosql

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/internal/badgeropen"
	"github.com/stretchr/testify/require"
)

// TestZeroWALCrashProcess 是受环境保护的真实持久子进程；不修改模块缓存或生产依赖。
func TestZeroWALCrashProcess(t *testing.T) {
	mode := os.Getenv("CORE_ZERO_WAL_PROCESS")
	if mode == "" {
		t.Skip("仅由零WAL真实硬杀父测试启动")
	}
	root := os.Getenv("CORE_ZERO_WAL_ROOT")
	s := openIndependentStore(t, filepath.Join(root, "store"), false, 2, &cleanupTestTarget{})
	if mode == "crash" {
		require.NoError(t, s.Save(context.Background(), groupRecord(t, "original", "group-A", 1, false, &cleanupTestProbe{})))
		var pending *PendingSyncError
		require.ErrorAs(t, s.Close(context.Background()), &pending)
		require.Equal(t, 1, pending.Count)
		// 验收断点副本只在此真实Badger删除空memtable时写断点并阻塞，父进程SIGKILL；不伪造恢复返回值。
		require.NoError(t, CloseSharedManager(s.config.BasePath))
		t.Fatal("未命中固定删除断点")
	}
	v, err := s.GetLocal(context.Background(), "original")
	require.NoError(t, err)
	require.Equal(t, 1, v.Revision)
	records, _, err := s.ListWriteBehindFailures("", 16)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, WriteBehindReady, records[0].State)
	require.Zero(t, records[0].Attempts)
	require.Equal(t, 1, s.Metrics().Pending)
	n, err := s.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, err)
	require.Equal(t, 1, n.Confirmed)
	require.Zero(t, n.Remaining)
	require.Zero(t, s.Metrics().Pending)
	require.NoError(t, s.Close(context.Background()))
	require.NoError(t, CloseSharedManager(s.config.BasePath))
	require.NoError(t, os.WriteFile(filepath.Join(root, "recovered"), []byte("原件及预算恢复，无新Save，一次真实确认"), 0600))
}

// TestZeroWALHardCrashRecovery 验证真实删除窗口硬杀、新PID首次重开原卷，并保留打开前的原始卷副本。
func TestZeroWALHardCrashRecovery(t *testing.T) {
	if os.Getenv("CORE_TEST_ZERO_WAL_CONTROLLED") != "1" {
		t.Skip("需显式启用并使用已封存Ristretto删除断点副本")
	}
	root := emptyWALFixtureRoot(t)
	exe, err := os.Executable()
	require.NoError(t, err)
	start := func(mode string) (*exec.Cmd, *os.File) {
		output, err := os.Create(filepath.Join(root, mode+".log"))
		require.NoError(t, err)
		cmd := exec.Command(exe, "-test.run=^TestZeroWALCrashProcess$", "-test.timeout=20s")
		cmd.Env = append(os.Environ(), "CORE_ZERO_WAL_PROCESS="+mode, "CORE_ZERO_WAL_ROOT="+root, "CORE_TEST_ZERO_WAL_DELETE_READY=")
		if mode == "crash" {
			cmd.Env = append(cmd.Env, "CORE_TEST_ZERO_WAL_DELETE_READY="+filepath.Join(root, "delete.ready"))
		}
		cmd.Stdout = output
		cmd.Stderr = output
		require.NoError(t, cmd.Start())
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			_ = output.Close()
			if t.Failed() {
				b, _ := os.ReadFile(output.Name())
				t.Logf("%s PID=%d %s", mode, cmd.Process.Pid, b)
			}
		})
		return cmd, output
	}
	crash, output := start("crash")
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(root, "delete.ready")); return err == nil }, 10*time.Second, time.Millisecond)
	b, err := os.ReadFile(filepath.Join(root, "delete.ready"))
	require.NoError(t, err)
	wal := string(b)
	require.True(t, filepath.IsLocal(mustRelativeWAL(t, root, wal)))
	info, err := os.Stat(wal)
	require.NoError(t, err)
	require.Zero(t, info.Size(), "真实Delete已truncate，尚未unlink")
	// child仍持真正Badger锁时，另一个PID不得借恢复入口修改空文件。
	blocked, lockErr := badgeropen.Open(badger.DefaultOptions(filepath.Dir(wal)).WithLogger(nil))
	require.Error(t, lockErr)
	require.Nil(t, blocked)
	info, err = os.Stat(wal)
	require.NoError(t, err)
	require.Zero(t, info.Size())
	require.NoError(t, crash.Process.Kill())
	require.Error(t, crash.Wait())
	require.NoError(t, output.Close())
	// 死进程原卷的打开前副本；后续失败不能靠第二次打开改变原件而丢证据。
	source := filepath.Join(root, "store")
	snapshot := filepath.Join(root, "before-reopen")
	require.NoError(t, filepath.WalkDir(source, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return e
		}
		dst := filepath.Join(snapshot, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0700)
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		return os.WriteFile(dst, data, 0600)
	}))
	recovered, output := start("recover")
	require.NotEqual(t, crash.Process.Pid, recovered.Process.Pid)
	require.NoError(t, recovered.Wait())
	require.NoError(t, output.Close())
	_, err = os.Stat(filepath.Join(root, "recovered"))
	require.NoError(t, err)
	t.Logf("固定真实删除窗口SIGKILL PID=%d，新PID=%d首次重开原卷通过，原卷副本=%s", crash.Process.Pid, recovered.Process.Pid, snapshot)
}

func mustRelativeWAL(t *testing.T, root, path string) string {
	t.Helper()
	r, e := filepath.Rel(root, path)
	require.NoError(t, e)
	return r
}
