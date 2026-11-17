// 本文件用独立PID、原生15秒租约和真实SQL行锁验证任务归属接管；不建立生产任务框架或金融规则。
package ownership_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestGroupWorkSQLProcesses复用当前测试可执行文件；两个真实owner有独立PID，不以同进程对象替代。
func TestGroupWorkSQLProcesses(t *testing.T) {
	base := os.Getenv("CORE_SQL_TASK_PROCESS_ROOT")
	if base == "" {
		t.Skip("NOT_RUN: CORE_SQL_TASK_PROCESS_ROOT and isolated Redis/MySQL")
	}
	if role := os.Getenv("CORE_SQL_TASK_PROCESS_ROLE"); role != "" {
		db := sqlTaskProcessDatabase(t)
		raw, err := os.ReadFile(filepath.Join(base, "manifest.json"))
		require.NoError(t, err)
		var m ownership.Manifest
		require.NoError(t, json.Unmarshal(raw, &m))
		ctx := context.Background()
		p := mq.NewRedisStreamProvider(os.Getenv("CORE_TEST_REDIS_ADDR"), m.Scope, 0)
		require.NoError(t, p.Connect(ctx))
		defer p.Close()
		store, err := p.OwnershipStore(ctx)
		require.NoError(t, err)
		budget, err := ownership.NewGroupWorkBudget(role, ownership.GroupWorkBudgetOptions{Workers: 2, Prefetch: 4})
		require.NoError(t, err)
		defer budget.Close(ctx)
		rt, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: role, SessionID: role + "-boot", MaxOwned: 1, Activate: func(ctx context.Context, c ownership.Credential) error {
			marker, _ := json.Marshal(map[string]any{"key": c.Key, "epoch": c.Epoch, "pid": os.Getpid()})
			if err := os.WriteFile(filepath.Join(base, "activating-"+role+".json"), marker, 0600); err != nil {
				return err
			}
			return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return ownership.ActivateFence(mysqlFenceTransaction{tx}, c) })
		}, GroupWork: &ownership.GroupWorkOptions{Budget: budget, NextGroups: func(ctx context.Context, c ownership.Credential, q ownership.GroupWorkRequest) ([]string, error) {
			return sqlReadyGroups(ctx, db, c, q)
		}, StepGroup: func(ctx context.Context, c ownership.Credential, g string) (bool, error) {
			return sqlGroupStep(ctx, db, c, g, nil)
		}}, Observe: func(ctx context.Context, c ownership.Credential) (ownership.Observation, error) {
			return taskObservation(ctx, db, c.Key)
		}})
		require.NoError(t, err)
		written := false
		for {
			if _, err := os.Stat(filepath.Join(base, "stop-"+role)); err == nil {
				wait, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				require.NoError(t, rt.Close(wait))
				return
			}
			if !written {
				for _, key := range m.Keys {
					status, e := rt.Status(ctx, key)
					if e == nil {
						b, e := json.Marshal(status)
						require.NoError(t, e)
						require.NoError(t, os.WriteFile(filepath.Join(base, "ready-"+role+".json"), b, 0600))
						written = true
						break
					}
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	db := mysqlFenceDatabase(t, &ownershipSQLTask{}).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	schema := db.Migrator().CurrentDatabase()
	root := filepath.Join(base, fmt.Sprintf("native-%d", time.Now().UnixNano()))
	require.NoError(t, os.MkdirAll(root, 0700))
	m := ownership.Manifest{Scope: fmt.Sprintf("task-process-%d", time.Now().UnixNano()), Service: "fixture", Group: "fixture", Selector: "opaque-v1", Keys: []string{"A", "B"}, Subjects: []string{"receipt"}}.Normalize()
	b, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "manifest.json"), b, 0600))
	p := mq.NewRedisStreamProvider(os.Getenv("CORE_TEST_REDIS_ADDR"), m.Scope, 0)
	ctx := context.Background()
	require.NoError(t, p.Connect(ctx))
	defer p.Close()
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	store, err := p.OwnershipStore(ctx)
	require.NoError(t, err)
	for _, key := range m.Keys {
		require.NoError(t, db.Create(&mysqlFenceFixture{Scope: m.Scope, KeyName: key}).Error)
	}
	held := []ownershipSQLTask{{OwnerKey: "A", OrderKey: "held", Ordinal: 1, State: "paused", Attempts: 2}, {OwnerKey: "A", OrderKey: "held", Ordinal: 2, State: "pending", Attempts: 3}}
	require.NoError(t, db.Create(&held).Error)
	unknown := []ownershipSQLTask{{OwnerKey: "A", OrderKey: "unknown", Ordinal: 1, State: "unknown", Attempts: 2, Writes: 1}, {OwnerKey: "A", OrderKey: "unknown", Ordinal: 2, State: "pending", Attempts: 3}}
	require.NoError(t, db.Create(&unknown).Error)
	healthyB := ownershipSQLTask{OwnerKey: "B", OrderKey: "ready", Ordinal: 1, State: "pending"}
	require.NoError(t, db.Create(&healthyB).Error)
	var children []*exec.Cmd
	var logs []*os.File
	defer func() {
		for _, c := range children {
			if c.ProcessState == nil {
				_ = c.Process.Kill()
				_ = c.Wait()
			}
		}
		for _, f := range logs {
			_ = f.Close()
		}
	}()
	start := func(role string) *exec.Cmd {
		f, e := os.Create(filepath.Join(root, role+".log"))
		require.NoError(t, e)
		logs = append(logs, f)
		cmd := exec.Command(os.Args[0], "-test.run=^TestGroupWorkSQLProcesses$", "-test.v", "-test.timeout=90s")
		cmd.Env = append(os.Environ(), "CORE_SQL_TASK_PROCESS_ROOT="+root, "CORE_SQL_TASK_PROCESS_ROLE="+role, "CORE_SQL_TASK_PROCESS_DATABASE="+schema)
		cmd.Stdout = f
		cmd.Stderr = f
		require.NoError(t, cmd.Start())
		children = append(children, cmd)
		return cmd
	}
	ready := func(role string) ownership.KeyStatus {
		var status ownership.KeyStatus
		require.Eventually(t, func() bool {
			raw, e := os.ReadFile(filepath.Join(root, "ready-"+role+".json"))
			return e == nil && json.Unmarshal(raw, &status) == nil
		}, m.Timing.Lease+5*time.Second, 10*time.Millisecond)
		return status
	}
	a := start("a-old")
	as := ready("a-old")
	require.Equal(t, "A", as.Key)
	bproc := start("b-old")
	bs := ready("b-old")
	require.Equal(t, "B", bs.Key)
	require.NotEqual(t, a.Process.Pid, bproc.Process.Pid)
	require.Eventually(t, func() bool {
		var task ownershipSQLTask
		return db.First(&task, healthyB.ID).Error == nil && task.Writes == 1
	}, 3*time.Second, 10*time.Millisecond)
	oldB, err := store.Resolve(ctx, m.Scope, "B")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "stop-b-old"), nil, 0600))
	require.NoError(t, bproc.Wait())
	bnext := start("b-new")
	bns := ready("b-new")
	require.Equal(t, "B", bns.Key)
	require.Greater(t, bns.Epoch, bs.Epoch)
	require.NotEqual(t, bnext.Process.Pid, bproc.Process.Pid)
	require.ErrorIs(t, store.Release(ctx, oldB), ownership.ErrFenced)
	require.ErrorIs(t, store.Renew(ctx, oldB, m.Timing.Lease), ownership.ErrFenced)
	oldA, err := store.Resolve(ctx, m.Scope, "A")
	require.NoError(t, err)
	// 受控旧凭据SQL连接保持真实行锁；不把Redis租约过期或进程硬杀当远端业务已停止。
	oldSQL := db.Begin()
	require.NoError(t, oldSQL.Error)
	defer oldSQL.Rollback()
	require.NoError(t, ownership.CheckFenceShared(mysqlSharedFenceTransaction{oldSQL}, oldA))
	require.NoError(t, a.Process.Kill())
	require.Error(t, a.Wait(), "必须实际SIGKILL，不使用正常退出替代")
	anew := start("a-new")
	var activating struct {
		Key   string `json:"key"`
		Epoch int64  `json:"epoch"`
	}
	require.Eventually(t, func() bool {
		raw, e := os.ReadFile(filepath.Join(root, "activating-a-new.json"))
		return e == nil && json.Unmarshal(raw, &activating) == nil
	}, m.Timing.Lease+5*time.Second, 10*time.Millisecond)
	require.Equal(t, "A", activating.Key)
	require.Greater(t, activating.Epoch, oldA.Epoch)
	_, err = store.Resolve(ctx, m.Scope, "A")
	require.ErrorIs(t, err, ownership.ErrNotReady, "新owner不能在旧SQL真实提交前Ready")
	require.NoError(t, oldSQL.Model(&mysqlFenceFixture{}).Where("scope = ? AND key_name = ?", m.Scope, "A").UpdateColumn("writes", gorm.Expr("writes + 1")).Error)
	require.NoError(t, oldSQL.Commit().Error)
	ans := ready("a-new")
	require.Greater(t, ans.Epoch, as.Epoch)
	require.NotEqual(t, anew.Process.Pid, a.Process.Pid)
	require.Nil(t, ans.LastConfirmed, "激活不是任务成功")
	nextA, err := store.Resolve(ctx, m.Scope, "A")
	require.NoError(t, err)
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return ownership.CheckFenceShared(mysqlSharedFenceTransaction{tx}, oldA) }), ownership.ErrFenced)
	wrong := nextA
	wrong.Token = "synthetic-wrong-token"
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return ownership.ActivateFence(mysqlFenceTransaction{tx}, wrong) }), ownership.ErrFenced)
	var kept []ownershipSQLTask
	require.NoError(t, db.Where("id IN ?", []uint{held[0].ID, held[1].ID}).Order("ordinal").Find(&kept).Error)
	require.Equal(t, "paused", kept[0].State)
	require.Equal(t, "pending", kept[1].State)
	require.EqualValues(t, 2, kept[0].Attempts)
	require.EqualValues(t, 3, kept[1].Attempts)
	require.Zero(t, kept[1].Writes)
	healthyA := ownershipSQLTask{OwnerKey: "A", OrderKey: "ready", Ordinal: 1, State: "pending"}
	require.NoError(t, db.Create(&healthyA).Error)
	require.Eventually(t, func() bool {
		var row ownershipSQLTask
		return db.First(&row, healthyA.ID).Error == nil && row.Writes == 1 && row.WriterSession == "a-new-boot"
	}, 3*time.Second, 10*time.Millisecond)

	var unknownKept []ownershipSQLTask
	require.NoError(t, db.Where("id IN ?", []uint{unknown[0].ID, unknown[1].ID}).Order("ordinal").Find(&unknownKept).Error)
	require.Equal(t, "unknown", unknownKept[0].State)
	require.EqualValues(t, 1, unknownKept[0].Writes)
	require.EqualValues(t, 2, unknownKept[0].Attempts)
	require.Equal(t, "pending", unknownKept[1].State)
	require.Zero(t, unknownKept[1].Writes)
	require.EqualValues(t, 3, unknownKept[1].Attempts)
	// 应用确认原SQL事实后只解除前序；新PID在无新请求时推进后继。
	require.NoError(t, db.Model(&ownershipSQLTask{}).Where("id = ?", unknown[0].ID).Update("state", "resolved").Error)
	require.Eventually(t, func() bool {
		var row ownershipSQLTask
		return db.First(&row, unknown[1].ID).Error == nil && row.Writes == 1 && row.WriterSession == "a-new-boot"
	}, 3*time.Second, 10*time.Millisecond)

	for _, role := range []string{"a-new", "b-new"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "stop-"+role), nil, 0600))
	}
	require.NoError(t, anew.Wait())
	require.NoError(t, bnext.Wait())
	t.Logf("independent PID oldA=%d newA=%d oldB=%d newB=%d; native lease=%s; original schema=%s; paused/pending/attempts retained; old SQL fence and late credentials rejected; evidence=%s", a.Process.Pid, anew.Process.Pid, bproc.Process.Pid, bnext.Process.Pid, m.Timing.Lease, schema, root)
}
