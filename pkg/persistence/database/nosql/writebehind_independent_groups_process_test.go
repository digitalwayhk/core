// 本文件复用既有测试进程与真实Badger协议，硬杀旧PID后在原卷恢复独立组，不靠新Save唤醒。
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

// TestIndependentGroupsRecoveryProcess 仅供父测试启动，应用持久事实和Core pending为两份独立真实库。
func TestIndependentGroupsRecoveryProcess(t *testing.T) {
	mode := os.Getenv("CORE_GROUP_PROCESS_MODE")
	if mode == "" {
		t.Skip("仅由独立组硬杀父测试启动")
	}
	root := os.Getenv("CORE_GROUP_PROCESS_ROOT")
	app, e := badger.Open(badger.DefaultOptions(filepath.Join(root, "app")).WithSyncWrites(true).WithLogger(nil))
	require.NoError(t, e)
	defer app.Close()
	if stage := os.Getenv("CORE_GROUP_CONFIRM_STAGE"); stage != "" {
		runIndependentConfirmationProcess(t, mode, root, stage, app)
		return
	}
	entered := make(chan struct{})
	target := &cleanupTestTarget{}
	target.sync = func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		key := items[0].Item.Key
		_, e := processAppCount(app, key, true)
		if e != nil {
			return nil, e
		}
		if mode == "crash" && key == "A1" {
			close(entered)
			select {}
		}
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}
	target.handle = func(_ context.Context, f WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error) {
		count, e := processAppCount(app, f.Record.Snapshot.Item.Key, false)
		if e != nil {
			return WriteBehindDecision{}, e
		}
		require.Equal(t, 1, count, "未知结果查真实应用事实，不盲重做")
		d := cleanupTestDecision(f.Record, WriteBehindConfirm)
		d.Outcome = WriteBehindCommitted
		return d, nil
	}
	s := openIndependentStore(t, filepath.Join(root, "core"), true, 2, target)
	if mode == "crash" {
		require.NoError(t, s.Save(context.Background(), groupRecord(t, "A1", "A", 1, false, &cleanupTestProbe{})))
		awaitGroupChannel(t, entered)
		for _, key := range []string{"B1", "B2"} {
			require.NoError(t, s.Save(context.Background(), groupRecord(t, key, "B", 1, false, &cleanupTestProbe{})))
			require.Eventually(t, func() bool { return s.Metrics().Pending == 1 }, time.Second, time.Millisecond)
		}
		require.NoError(t, os.WriteFile(filepath.Join(root, "crash.ready"), []byte("A-target-committed-B1-B2-locally-confirmed"), 0600))
		select {}
	}
	require.Eventually(t, func() bool { return s.Metrics().Pending == 0 }, 3*time.Second, time.Millisecond)
	records, _, e := s.ListWriteBehindFailures("", 16)
	require.NoError(t, e)
	require.Len(t, records, 3)
	for _, r := range records {
		require.Equal(t, WriteBehindCompleted, r.State)
		require.True(t, r.LocalConfirmed)
		require.Equal(t, 1, r.Attempts)
	}
	for _, key := range []string{"A1", "B1", "B2"} {
		n, e := processAppCount(app, key, false)
		require.NoError(t, e)
		require.Equal(t, 1, n, "已确认项不可重放；未知项由应用确认")
	}
	require.NoError(t, s.Close(context.Background()))
	require.NoError(t, CloseSharedManager(s.config.BasePath))
	require.NoError(t, os.WriteFile(filepath.Join(root, "recover.ready"), []byte("same-volume-no-new-save"), 0600))
}

// TestIndependentGroupsHardCrashRecovery 验证A未知、健康B已确认的混合状态在新PID原目录恢复，保持attempt和幂等事实。
func TestIndependentGroupsHardCrashRecovery(t *testing.T) {
	for _, stage := range []string{"", "before_confirmation", "after_confirmation"} {
		t.Run("stage="+stage, func(t *testing.T) {
			root := t.TempDir()
			exe, e := os.Executable()
			require.NoError(t, e)
			start := func(mode string) (*exec.Cmd, *os.File) {
				output, e := os.Create(filepath.Join(root, mode+".log"))
				require.NoError(t, e)
				cmd := exec.Command(exe, "-test.run=^TestIndependentGroupsRecoveryProcess$", "-test.timeout=20s")
				cmd.Env = append(os.Environ(), "CORE_GROUP_PROCESS_MODE="+mode, "CORE_GROUP_PROCESS_ROOT="+root, "CORE_GROUP_CONFIRM_STAGE="+stage)
				cmd.Stdout = output
				cmd.Stderr = output
				require.NoError(t, cmd.Start())
				t.Cleanup(func() {
					_ = cmd.Process.Kill()
					_ = output.Close()
					if t.Failed() {
						b, _ := os.ReadFile(output.Name())
						t.Logf("mode=%s PID=%d %s", mode, cmd.Process.Pid, b)
					}
				})
				return cmd, output
			}
			crash, output := start("crash")
			require.Eventually(t, func() bool { _, e := os.Stat(filepath.Join(root, "crash.ready")); return e == nil }, 10*time.Second, time.Millisecond)
			require.NoError(t, crash.Process.Kill())
			require.Error(t, crash.Wait())
			require.NoError(t, output.Close())
			recover, output := start("recover")
			require.NotEqual(t, crash.Process.Pid, recover.Process.Pid)
			require.NoError(t, recover.Wait())
			require.NoError(t, output.Close())
			_, e = os.Stat(filepath.Join(root, "recover.ready"))
			require.NoError(t, e)
			t.Logf("stage=%s，SIGKILL旧PID=%d，新PID=%d；子进程核对原事实/attempt/已确认不重放，原卷无新Save恢复", stage, crash.Process.Pid, recover.Process.Pid)

		})
	}
}
