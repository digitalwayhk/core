// 本文件复用现有测试子进程模式，验证v1.9.0部分迁移卷在硬杀后由候选和原版继续。
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

func emptyGroupProcessStore(t *testing.T, root string) *ReliableWriteStore[confirmationLoadRecord] {
	t.Helper()
	cfg := DefaultProductionConfig(root)
	cfg.AutoSync = false
	cfg.EnableLogger = false
	cfg.WriteBehindFailure = &WriteBehindFailurePolicy{StoreID: "group-operations", MaxAttempts: 3, MinRetryInterval: time.Second, MaxRetryInterval: 10 * time.Second, HandlerMaxAttempts: 2, HandlerTimeout: time.Second, HandlerWorkers: 1, ScanLimit: 128}
	s, _, err := NewReliableWriteStore[confirmationLoadRecord](ServiceIdentity{ServiceName: "group-operations"}, ReliableWriteStoreConfig{BasePath: root, Badger: cfg, Batch: BatchCommitConfig{MaxBatch: 128, CollectBacklog: 1, CollectWindow: time.Millisecond, QueueCapacity: 1024}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()); _ = CloseSharedManager(s.config.BasePath) })
	return s
}

// TestEmptyGroupMigrationProcess只接受父测试角色；使用真实PID、原目录、持久检查点，不用对象重建冒充。
func TestEmptyGroupMigrationProcess(t *testing.T) {
	role := os.Getenv("CORE_EMPTY_GROUP_PROCESS_ROLE")
	if role == "" {
		t.Skip("独立进程迁移恢复夹具")
	}
	root := os.Getenv("CORE_EMPTY_GROUP_PROCESS_ROOT")
	s := emptyGroupProcessStore(t, root)
	a := &confirmationLoadTarget{}
	if role == "seed" {
		require.NoError(t, s.UseWriteBehindWithFailureHandler(a, a))
		emptyGroupMigrationRecords(t, s, 24)
		require.NoError(t, s.Save(context.Background(), groupOperationRecord("unresolved", 1, 64)))
		r := currentGroupRecord(t, s, "unresolved")
		r.State = WriteBehindPaused
		r.Attempts = 2
		require.NoError(t, s.db.updateWriteBehindTransaction(func(txn *badger.Txn) error {
			if err := s.db.saveDisposition(txn, &r); err != nil {
				return err
			}
			if err := dispositionWrite(txn, s.db.dispositionPrefix()+"history-init", ""); err != nil {
				return err
			}
			return dispositionWrite(txn, s.db.dispositionPrefix()+"history-batches-init", "")
		}))
		return
	}
	original := s.db.localSubmit
	if role == "crash" {
		s.db.localSubmit = func(fn func() error) error {
			err := original(fn)
			if err != nil {
				return err
			}
			var cursor string
			require.NoError(t, s.db.manager.db.View(func(txn *badger.Txn) error {
				return dispositionRead(txn, s.db.dispositionPrefix()+"history-init", &cursor)
			}))
			if cursor != "" && cursor != "done" {
				require.NoError(t, os.WriteFile(filepath.Join(root, "crash.ready"), []byte(cursor), 0600))
				select {}
			}
			return nil
		}
	}
	require.NoError(t, s.UseWriteBehindWithFailureHandler(a, a))
	require.Contains(t, []string{"recover", "verify"}, role)
	var cursor, mode string
	require.NoError(t, s.db.manager.db.View(func(txn *badger.Txn) error {
		if err := dispositionRead(txn, s.db.dispositionPrefix()+"history-init", &cursor); err != nil {
			return err
		}
		return dispositionRead(txn, s.db.dispositionPrefix()+"mode", &mode)
	}))
	require.Equal(t, "done", cursor)
	require.True(t, strings.HasPrefix(mode, terminalDispositionFormat))
	r := currentGroupRecord(t, s, "unresolved")
	require.Equal(t, WriteBehindPaused, r.State)
	require.Equal(t, 2, r.Attempts)
	require.NotNil(t, r.Snapshot.Item)
	requireDispositionGroup(t, s.db, 1, true)
	require.Equal(t, 1, s.Metrics().Pending)
	if role == "recover" {
		result, err := s.ForceSyncBatch(context.Background(), 128)
		require.NoError(t, err)
		require.Zero(t, result.Confirmed, "不能越过原暂停前序")
		require.NoError(t, s.Save(context.Background(), groupOperationRecord("independent", 2, 64)))
		result, err = s.ForceSyncBatch(context.Background(), 128)
		require.NoError(t, err)
		require.Equal(t, 1, result.Confirmed, "无关组必须仍前进")
	}
	usage, err := s.WriteBehindHistoryUsage(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), usage.UnresolvedRecords)
	require.Equal(t, int64(1), usage.Pending)
	raw, err := json.Marshal(map[string]any{"pid": os.Getpid(), "role": role, "cursor": cursor, "mode_compatible": true, "usage": usage})
	require.NoError(t, err)
	t.Log(string(raw))
}

// TestEmptyGroupMigrationHardCrashContinuation 用原v1.9.0二进制建卷、候选硬杀/恢复，再原二进制回开同代格式。
func TestEmptyGroupMigrationHardCrashContinuation(t *testing.T) {
	baseline := os.Getenv("CORE_EMPTY_GROUP_BASELINE_BINARY")
	if baseline == "" {
		t.Skip("需提供精确v1.9.0加测试夹具二进制，不能把未运行当通过")
	}
	current, err := os.Executable()
	require.NoError(t, err)
	root := t.TempDir()
	run := func(binary, role string) *exec.Cmd {
		cmd := exec.Command(binary, "-test.run=^TestEmptyGroupMigrationProcess$", "-test.v", "-test.timeout=30s")
		cmd.Env = append(os.Environ(), "CORE_EMPTY_GROUP_PROCESS_ROOT="+root, "CORE_EMPTY_GROUP_PROCESS_ROLE="+role)
		return cmd
	}
	seed := run(baseline, "seed")
	out, err := seed.CombinedOutput()
	require.NoError(t, err, "原v1.9.0建卷: %s", out)
	crash := run(current, "crash")
	log, err := os.Create(filepath.Join(root, "crash.log"))
	require.NoError(t, err)
	crash.Stdout = log
	crash.Stderr = log
	require.NoError(t, crash.Start())
	t.Cleanup(func() { _ = crash.Process.Kill(); _ = log.Close() })
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(root, "crash.ready")); return err == nil }, 10*time.Second, time.Millisecond)
	require.NoError(t, crash.Process.Kill())
	require.Error(t, crash.Wait())
	require.NoError(t, log.Close())
	recover := run(current, "recover")
	out, err = recover.CombinedOutput()
	require.NoError(t, err, "候选新PID恢复: %s", out)
	t.Log(string(out))
	verify := run(baseline, "verify")
	out, err = verify.CombinedOutput()
	require.NoError(t, err, "原v1.9.0新PID复开: %s", out)
	t.Log(string(out))
	t.Logf("原版建卷PID=%d 候选硬杀PID=%d 候选恢复PID=%d 原版复开PID=%d", seed.Process.Pid, crash.Process.Pid, recover.Process.Pid, verify.Process.Pid)
}
