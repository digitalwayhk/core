// 本文件复用现有独立测试进程夹具，通过硬杀验证真实旧卷导入/维护中断恢复，不接触消费方。
package nosql

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

// TestTerminalHistoryProcess供父测试启动，断点在真实持久提交之后、结果返回之前。
func TestTerminalHistoryProcess(t *testing.T) {
	role := os.Getenv("CORE_TERMINAL_PROCESS_ROLE")
	if role == "" {
		t.Skip("独立进程恢复夹具")
	}
	root := os.Getenv("CORE_TERMINAL_ROOT")
	s := terminalLegacyFixtureStore(t, root)
	beforeUsage, e := s.WriteBehindHistoryUsage(context.Background())
	require.NoError(t, e)
	if role != "recover" {
		require.Greater(t, beforeUsage.TerminalPayloadBytes, int64(0), "旧卷在绑定前必须能测到原终态载荷")
	}
	original := s.db.localSubmit
	if role == "migration_crash" || role == "compaction_crash" || role == "batch_migration_crash" {
		s.db.localSubmit = func(fn func() error) error {
			e := original(fn)
			if e != nil {
				return e
			}
			var progress string
			marker := "history-init"
			if role == "batch_migration_crash" {
				marker = "history-batches-init"
			}
			if role == "compaction_crash" {
				marker = "history-cursor"
			}
			_ = s.db.manager.db.View(func(txn *badger.Txn) error {
				if role == "compaction_crash" {
					opts := badger.DefaultIteratorOptions
					opts.PrefetchValues = false
					opts.Prefix = []byte(s.db.dispositionPrefix() + "r:")
					it := txn.NewIterator(opts)
					defer it.Close()
					n := 0
					for it.Seek(opts.Prefix); it.ValidForPrefix(opts.Prefix); it.Next() {
						n++
					}
					if n > 0 && n < 98 {
						progress = "committed-terminal-removal"
					}
					return nil
				}
				return dispositionRead(txn, s.db.dispositionPrefix()+marker, &progress)
			})
			if progress != "" && progress != "done" {
				require.NoError(t, os.WriteFile(filepath.Join(root, "crash.ready"), []byte(role), 0600))
				select {}
			}
			return nil
		}
	}
	target := terminalLegacyFixtureTarget()
	require.NoError(t, s.UseWriteBehindWithFailureHandler(target, target))
	afterBindUsage, e := s.WriteBehindHistoryUsage(context.Background())
	require.NoError(t, e)
	require.Zero(t, afterBindUsage.TerminalPayloadBytes)
	before, _, e := s.ListWriteBehindFailures("", 1000)
	require.NoError(t, e)
	for _, r := range before {
		if dispositionTerminal(r.State) {
			require.Nil(t, r.Snapshot.Item)
		} else {
			require.NotNil(t, r.Snapshot.Item)
		}
	}
	compactTerminalTestCycle(t, s, 2)
	r, _, e := s.ListWriteBehindFailures("", 1000)
	require.NoError(t, e)
	require.Len(t, r, 2)
	require.Equal(t, 2, s.Metrics().Pending)
	for _, r := range r {
		require.NotNil(t, r.Snapshot.Item)
		require.False(t, dispositionTerminal(r.State))
		if r.State == WriteBehindPaused {
			require.Equal(t, 1, r.Attempts)
		}
	}
	afterUsage, e := s.WriteBehindHistoryUsage(context.Background())
	require.NoError(t, e)
	require.Equal(t, beforeUsage.BusinessRecords, afterUsage.BusinessRecords)
	require.Equal(t, beforeUsage.UnresolvedRecords, afterUsage.UnresolvedRecords)
	require.Equal(t, beforeUsage.Pending, afterUsage.Pending)
	var physical int64
	require.NoError(t, filepath.Walk(s.config.BasePath, func(_ string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.Mode().IsRegular() {
			physical += info.Size()
		}
		return nil
	}))
	gc, e := (&ReliableWriteStoreAdmin[cleanupTestRecord]{db: s.db}).ReclaimStorage(context.Background(), 10)
	require.NoError(t, e)
	var afterPhysical int64
	require.NoError(t, filepath.Walk(s.config.BasePath, func(_ string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.Mode().IsRegular() {
			afterPhysical += info.Size()
		}
		return nil
	}))
	report, _ := json.Marshal(map[string]any{"history_usage_before_bind": beforeUsage, "history_usage_after_bind": afterBindUsage, "history_usage_after_compaction": afterUsage, "physical_before_gc": physical, "physical_after_gc": afterPhysical, "gc": gc})
	t.Log(string(report))
	n, e := s.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, e)
	require.Zero(t, n.Confirmed, "无新Save恢复不能越过原暂停组首")
}

// TestTerminalHistoryHardCrashMigration使用真正v1.8.1进程建卷，再硬杀新进程、第三进程重开原目录，并让旧二进制验证拒绝。
func TestTerminalHistoryHardCrashMigration(t *testing.T) {
	old := os.Getenv("CORE_TERMINAL_OLD_BINARY")
	if old == "" {
		t.Skip("须提供精确v1.8.1独立测试二进制，不能用同进程对象冒充")
	}
	exe, e := os.Executable()
	require.NoError(t, e)
	for _, stage := range []string{"migration_crash", "batch_migration_crash", "compaction_crash"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			run := func(binary, role string, legacy bool) *exec.Cmd {
				test := "^TestTerminalHistoryProcess$"
				env := "CORE_TERMINAL_PROCESS_ROLE="
				if legacy {
					test = "^TestTerminalLegacyVolumeFixture$"
					env = "CORE_TERMINAL_LEGACY_ROLE="
				}
				cmd := exec.Command(binary, "-test.run="+test, "-test.v", "-test.timeout=30s")
				cmd.Env = append(os.Environ(), env+role, "CORE_TERMINAL_ROOT="+root)
				return cmd
			}
			seed := run(old, "seed", true)
			data, e := seed.CombinedOutput()
			require.NoError(t, e, "旧PID建卷: %s", data)
			crash := run(exe, stage, false)
			log, e := os.Create(filepath.Join(root, "crash.log"))
			require.NoError(t, e)
			crash.Stdout = log
			crash.Stderr = log
			require.NoError(t, crash.Start())
			t.Cleanup(func() { _ = crash.Process.Kill(); _ = log.Close() })
			require.Eventually(t, func() bool { _, e := os.Stat(filepath.Join(root, "crash.ready")); return e == nil }, 15*time.Second, time.Millisecond)
			require.NoError(t, crash.Process.Kill())
			require.Error(t, crash.Wait())
			require.NoError(t, log.Close())
			recover := run(exe, "recover", false)
			data, e = recover.CombinedOutput()
			require.NoError(t, e, "新PID恢复: %s", data)
			for _, line := range strings.Split(string(data), "\n") {
				if strings.Contains(line, "history_usage_before_bind") {
					t.Log(line)
				}
			}
			reject := run(old, "reject", true)
			data, e = reject.CombinedOutput()
			require.NoError(t, e, "旧二进制应明确拒绝新格式: %s", data)
			t.Logf("旧建卷PID=%d 硬杀PID=%d 新恢复PID=%d 旧拒绝PID=%d", seed.Process.Pid, crash.Process.Pid, recover.Process.Pid, reject.Process.Pid)
		})
	}
}
