// 本文件以独立测试进程硬杀及两份真实Badger验证交接前后崩溃，不使用模拟可信状态。
package nosql

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

func processAppCount(app *badger.DB, key string, increment bool) (int, error) {
	value := 0
	err := app.Update(func(txn *badger.Txn) error {
		e := dispositionRead(txn, "count:"+key, &value)
		if e != nil && !errors.Is(e, badger.ErrKeyNotFound) {
			return e
		}
		if increment {
			value++
			return dispositionWrite(txn, "count:"+key, value)
		}
		return nil
	})
	return value, err
}

// TestConfirmationCrashProcess 仅供父测试以独立PID启动；断点通过文件通知后由父进程SIGKILL。
func TestConfirmationCrashProcess(t *testing.T) {
	mode := os.Getenv("CORE_CONFIRM_CRASH_MODE")
	if mode == "" {
		t.Skip("仅供Core确认硬崩溃父测试启动")
	}
	stage := os.Getenv("CORE_CONFIRM_CRASH_STAGE")
	path := os.Getenv("CORE_CONFIRM_CRASH_ROOT")
	ready := func() {
		require.NoError(t, os.WriteFile(filepath.Join(path, mode+".ready"), []byte(stage), 0600))
		select {}
	}
	app, err := badger.Open(badger.DefaultOptions(filepath.Join(path, "app")).WithLogger(nil).WithSyncWrites(true))
	require.NoError(t, err)
	defer app.Close()
	target := &cleanupTestTarget{}
	target.sync = func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		_, e := processAppCount(app, "effect", true)
		require.NoError(t, e)
		if mode == "crash" && stage == "before_confirmation" {
			ready()
		}
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}
	target.handle = func(_ context.Context, f WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error) {
		count, e := processAppCount(app, "effect", false)
		require.NoError(t, e)
		require.Equal(t, 1, count, "未知结果须查询应用持久事实，不能盲重做")
		d := cleanupTestDecision(f.Record, WriteBehindConfirm)
		d.Outcome = WriteBehindCommitted
		return d, nil
	}
	// unknown_result固定前台可靠提交已返回到提交器、尚未回复Save的窗口；恢复时无新Save。
	store := openCleanupTestStore(t, filepath.Join(path, "core"), mode == "recover", true, target)
	if mode == "crash" {
		probe := &cleanupTestProbe{}
		if stage == "after_confirmation" {
			probe.hook = func(*cleanupTestRecord) { ready() }
		}
		if stage == "unknown_result" {
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
		_, e := store.ForceSyncBatch(context.Background(), 16)
		require.NoError(t, e)
		select {}
	}
	if stage == "after_confirmation" {
		require.Zero(t, store.Metrics().Pending)
		v, e := store.GetLocal(context.Background(), "original")
		require.NoError(t, e)
		require.True(t, v.DeleteAfter, "确认后尚未完成物理清理，重启不伪造自动补删")
	} else {
		require.Eventually(t, func() bool { return store.Metrics().Pending == 0 }, 5*time.Second, time.Millisecond)
	}
	count, err := processAppCount(app, "effect", false)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	records, _, err := store.ListWriteBehindFailures("", 16)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, WriteBehindCompleted, records[0].State)
	require.True(t, records[0].LocalConfirmed)
	require.Equal(t, 1, records[0].Attempts)
	require.NoError(t, store.Close(context.Background()))
	require.NoError(t, CloseSharedManager(store.config.BasePath))
	require.NoError(t, os.WriteFile(filepath.Join(path, "recover.ready"), []byte("reopened-original-directory-without-new-save"), 0600))
}

// TestConfirmationHardCrashRecovery 固定target成功/确认成功/Save结果未知三个窗口，必须由新进程打开原目录。
func TestConfirmationHardCrashRecovery(t *testing.T) {
	for _, stage := range []string{"before_confirmation", "after_confirmation", "unknown_result"} {
		t.Run(stage, func(t *testing.T) {
			path := t.TempDir()
			exe, err := os.Executable()
			require.NoError(t, err)
			start := func(mode string) (*exec.Cmd, *os.File) {
				log, err := os.Create(filepath.Join(path, mode+".log"))
				require.NoError(t, err)
				cmd := exec.Command(exe, "-test.run=^TestConfirmationCrashProcess$", "-test.timeout=20s")
				cmd.Env = append(os.Environ(), "CORE_CONFIRM_CRASH_MODE="+mode, "CORE_CONFIRM_CRASH_STAGE="+stage, "CORE_CONFIRM_CRASH_ROOT="+path)
				cmd.Stdout = log
				cmd.Stderr = log
				require.NoError(t, cmd.Start())
				t.Cleanup(func() {
					_ = cmd.Process.Kill()
					_ = log.Close()
					if t.Failed() {
						b, _ := os.ReadFile(log.Name())
						t.Logf("%s PID=%d: %s", mode, cmd.Process.Pid, b)
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
			_, err = os.Stat(filepath.Join(path, "recover.ready"))
			require.NoError(t, err)
			t.Logf("真实SIGKILL PID=%d，新PID=%d，原目录恢复，无新前台请求", crash.Process.Pid, recover.Process.Pid)
		})
	}
}

// TestDispositionCrashProcess 是受显式环境变量保护的子进程夹具，普通测试不启动服务或访问外部存储。
func TestDispositionCrashProcess(t *testing.T) {
	stage := os.Getenv("CORE_DISPOSITION_HELPER")
	if stage == "" {
		t.Skip("仅由Core独立进程崩溃验收启动")
	}
	app, err := badger.Open(badger.DefaultOptions(os.Getenv("CORE_DISPOSITION_APP")).WithLogger(nil).WithSyncWrites(true))
	require.NoError(t, err)
	defer app.Close()
	a := &failureTestTarget{}
	a.sync = func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		keys := []string{}
		for _, v := range items {
			_, e := processAppCount(app, v.Item.UserID, true)
			require.NoError(t, e)
			if v.Item.UserID != "first" {
				keys = append(keys, v.Key)
			}
		}
		return &WriteBehindResult{ConfirmedKeys: keys}, errors.New("测试原业务结果未知")
	}
	a.handle = func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		r := durableTestReceipt(t, app, f)
		if stage == "before" {
			require.NoError(t, os.WriteFile(os.Getenv("CORE_DISPOSITION_READY"), []byte(f.Record.ID), 0600))
			select {}
		}
		d := failureDecision(f, WriteBehindHandoff)
		d.Receipt = r
		return d, nil
	}
	db := openFailureTestDB(t, os.Getenv("CORE_DISPOSITION_CORE"), true, a)
	if stage != "recover" {
		require.NoError(t, db.Set(newFund("first", "ordered", 1), 0))
		require.NoError(t, db.Set(newFund("later", "ordered", 1), 0))
	}
	if stage != "before" {
		require.Eventually(t, func() bool { m, e := db.GetWriteBehindDispositionMetrics(); return e == nil && m.HandedOff == 1 }, 5*time.Second, time.Millisecond)
		require.NoError(t, os.WriteFile(os.Getenv("CORE_DISPOSITION_READY"), []byte("core-receipt-persisted"), 0600))
	}
	select {}
}

// TestCrashBeforeAndAfterHandoffConfirmation 验证真实硬杀前后原身份、预算、回执及组屏障跨进程保留。
func TestCrashBeforeAndAfterHandoffConfirmation(t *testing.T) {
	for _, stage := range []string{"before", "after"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			corePath := filepath.Join(root, "core")
			appPath := filepath.Join(root, "app")
			exe, err := os.Executable()
			require.NoError(t, err)
			runAndKill := func(mode string) {
				t.Helper()
				ready := filepath.Join(root, mode+".ready")
				logPath := filepath.Join(root, mode+".log")
				output, e := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
				require.NoError(t, e)
				cmd := exec.Command(exe, "-test.run=^TestDispositionCrashProcess$", "-test.timeout=20s")
				cmd.Env = append(os.Environ(), "CORE_DISPOSITION_HELPER="+mode, "CORE_DISPOSITION_CORE="+corePath, "CORE_DISPOSITION_APP="+appPath, "CORE_DISPOSITION_READY="+ready)
				cmd.Stdout = output
				cmd.Stderr = output
				require.NoError(t, cmd.Start())
				defer func() {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					_ = output.Close()
					if t.Failed() {
						b, _ := os.ReadFile(logPath)
						t.Logf("子进程%s输出:\n%s", mode, b)
					}
				}()
				require.Eventually(t, func() bool { _, e := os.Stat(ready); return e == nil }, 10*time.Second, 5*time.Millisecond, "独立子进程未到达持久化断点")
				require.NoError(t, cmd.Process.Kill())
				err := cmd.Wait()
				require.Error(t, err)
				require.NoError(t, output.Close())
				t.Logf("独立PID=%d，stage=%s，真实硬杀后保留原Core/app卷", cmd.Process.Pid, mode)
			}
			runAndKill(stage)
			runAndKill("recover")
			app, err := badger.Open(badger.DefaultOptions(appPath).WithLogger(nil).WithSyncWrites(true))
			require.NoError(t, err)
			defer app.Close()
			count, err := processAppCount(app, "first", false)
			require.NoError(t, err)
			require.Equal(t, 1, count, "Core重启不能重做已由应用持久接管的target")
			count, err = processAppCount(app, "later", false)
			require.NoError(t, err)
			require.Zero(t, count, "交接回执不解除组屏障")
			a := &failureTestTarget{sync: func(context.Context, []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
				t.Error("接管后不应再调用target")
				return nil, nil
			}, handle: func(context.Context, WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
				t.Error("已保存回执不能再次通知处置器")
				return WriteBehindDecision{}, nil
			}}
			db := openFailureTestDB(t, corePath, false, a)
			records, _, err := db.ListWriteBehindFailures("", 10)
			require.NoError(t, err)
			require.Len(t, records, 2)
			for _, r := range records {
				if r.Snapshot.Item.UserID == "first" {
					require.Equal(t, WriteBehindHandedOff, r.State)
					require.Equal(t, 1, r.Attempts)
					if stage == "before" {
						require.Equal(t, 2, r.HandlerAttempts)
					} else {
						require.Equal(t, 1, r.HandlerAttempts)
					}
					require.NotNil(t, r.Receipt)
					var saved struct {
						Record  WriteBehindFailureRecord[testFund]
						Receipt WriteBehindHandoffReceipt
					}
					require.NoError(t, app.View(func(txn *badger.Txn) error { return dispositionRead(txn, r.ID, &saved) }))
					require.Equal(t, saved.Receipt, *r.Receipt)
					require.Equal(t, float64(1), saved.Record.Snapshot.Item.Balance)
				}
			}
			require.Equal(t, 1, db.GetCachedPendingSyncCount())
			require.Zero(t, db.GetSyncMetrics().SyncedItems)
		})
	}
}
