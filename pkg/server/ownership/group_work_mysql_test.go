// 本文件在原SQL事实表内发现组身份；真实组首仅在固定worker接纳后查询，不能把发现当claim。
package ownership_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func sqlReadyGroups(ctx context.Context, db *gorm.DB, c ownership.Credential, q ownership.GroupWorkRequest) ([]string, error) {
	table := db.NamingStrategy.TableName("ownershipSQLTask")
	query := db.WithContext(ctx).Table(table+" AS task").Select("DISTINCT task.order_key").Where("task.owner_key = ? AND task.state = ?", c.Key, "pending").Where("NOT EXISTS (SELECT 1 FROM "+table+" AS prior WHERE prior.owner_key = task.owner_key AND prior.order_key = task.order_key AND prior.ordinal < task.ordinal AND prior.state NOT IN ?)", []string{"done", "resolved"})
	if len(q.ExcludedGroups) > 0 {
		query = query.Where("task.order_key NOT IN ?", q.ExcludedGroups)
	}
	var groups []string
	err := query.Order("task.order_key").Limit(q.Limit).Pluck("task.order_key", &groups).Error
	return groups, err
}

// sqlGroupStep是测试应用的原SQL组首事务，不是Core业务状态机。before在真实头查询后、更新前执行。
func sqlGroupStep(ctx context.Context, db *gorm.DB, c ownership.Credential, g string, before func(ownershipSQLTask) error) (bool, error) {
	progress := false
	table := db.NamingStrategy.TableName("ownershipSQLTask")
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ownership.CheckFenceShared(mysqlSharedFenceTransaction{tx}, c); err != nil {
			return err
		}
		var task ownershipSQLTask
		err := tx.Table(table+" AS task").Clauses(clause.Locking{Strength: "UPDATE"}).Where("task.owner_key = ? AND task.order_key = ? AND task.state = ?", c.Key, g, "pending").Where("NOT EXISTS (SELECT 1 FROM "+table+" AS prior WHERE prior.owner_key = task.owner_key AND prior.order_key = task.order_key AND prior.ordinal < task.ordinal AND prior.state NOT IN ?)", []string{"done", "resolved"}).Order("task.ordinal").Take(&task).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if before != nil {
			if err := before(task); err != nil {
				return err
			}
		}
		result := tx.Model(&ownershipSQLTask{}).Where("id = ? AND state = ?", task.ID, "pending").Updates(map[string]any{"state": "done", "writes": gorm.Expr("writes+1"), "writer_session": c.SessionID})
		progress = result.RowsAffected == 1
		return result.Error
	})
	return progress, err
}

func TestGroupWorkMySQLIndependentCommitAndUnknown(t *testing.T) {
	db := mysqlFenceDatabase(t, &ownershipSQLTask{}).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	store, m := groupWorkFixture(t, fastGroupTiming())
	ctx := context.Background()
	budget := budgetForTest(t, 3, 4)
	require.NoError(t, db.Create(&mysqlFenceFixture{Scope: m.Scope, KeyName: "bucket"}).Error)
	tasks := []ownershipSQLTask{
		{OwnerKey: "bucket", OrderKey: "A", Ordinal: 1, State: "pending", Attempts: 2},
		{OwnerKey: "bucket", OrderKey: "A", Ordinal: 2, State: "pending", Attempts: 3},
		{OwnerKey: "bucket", OrderKey: "B", Ordinal: 1, State: "pending"},
		{OwnerKey: "bucket", OrderKey: "B", Ordinal: 2, State: "pending"},
		{OwnerKey: "bucket", OrderKey: "C", Ordinal: 1, State: "paused", Attempts: 2},
		{OwnerKey: "bucket", OrderKey: "C", Ordinal: 2, State: "pending", Attempts: 3},
		{OwnerKey: "bucket", OrderKey: "D", Ordinal: 1, State: "pending", Attempts: 2},
		{OwnerKey: "bucket", OrderKey: "D", Ordinal: 2, State: "pending", Attempts: 3},
		{OwnerKey: "bucket", OrderKey: "E", Ordinal: 1, State: "pending", Attempts: 2},
		{OwnerKey: "bucket", OrderKey: "E", Ordinal: 2, State: "pending", Attempts: 3},
	}
	require.NoError(t, db.Create(&tasks).Error)
	hold := make(chan struct{})
	aEntered := make(chan struct{})
	var once sync.Once
	var aQueries atomic.Int64
	var dCalls atomic.Int64
	runtime, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "sql-boot", MaxOwned: 1,
		Activate: func(ctx context.Context, c ownership.Credential) error {
			return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return ownership.ActivateFence(mysqlFenceTransaction{tx}, c) })
		},
		GroupWork: &ownership.GroupWorkOptions{Budget: budget, NextGroups: func(ctx context.Context, c ownership.Credential, q ownership.GroupWorkRequest) ([]string, error) {
			return sqlReadyGroups(ctx, db, c, q)
		},
			StepGroup: func(ctx context.Context, c ownership.Credential, g string) (bool, error) {

				if g == "E" {
					failedCommit := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
						if err := ownership.CheckFenceShared(mysqlSharedFenceTransaction{tx}, c); err != nil {
							return err
						}
						if err := tx.Model(&ownershipSQLTask{}).Where("id = ?", tasks[8].ID).UpdateColumn("writes", gorm.Expr("writes+1")).Error; err != nil {
							return err
						}
						return errors.New("synthetic business transaction rolled back")
					})
					if failedCommit == nil {
						return false, errors.New("rollback not observed")
					}
					// 失败预算与暂停由测试应用原SQL持久决定，Core不替应用分类或修改attempt。
					err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
						if err := ownership.CheckFenceShared(mysqlSharedFenceTransaction{tx}, c); err != nil {
							return err
						}
						return tx.Model(&ownershipSQLTask{}).Where("id = ? AND state = ?", tasks[8].ID, "pending").Updates(map[string]any{"state": "paused", "attempts": gorm.Expr("attempts+1")}).Error
					})
					if err != nil {
						return false, err
					}
					return false, failedCommit
				}
				if g == "D" {
					dCalls.Add(1)
					// 提交已成功但回执丢失；应用将未知前序持久保留，不能把后继当可执行。
					err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
						if e := ownership.CheckFenceShared(mysqlSharedFenceTransaction{tx}, c); e != nil {
							return e
						}
						return tx.Model(&ownershipSQLTask{}).Where("id = ? AND state = ?", tasks[6].ID, "pending").Updates(map[string]any{"state": "unknown", "writes": gorm.Expr("writes+1")}).Error
					})
					if err != nil {
						return false, err
					}
					return false, errors.New("synthetic commit reply lost")
				}
				return sqlGroupStep(ctx, db, c, g, func(row ownershipSQLTask) error {
					if g == "A" {
						aQueries.Add(1)
						if row.Ordinal == 1 {
							close(aEntered)
							select {
							case <-hold:
							case <-ctx.Done():
								return ctx.Err()
							}
						}
					}
					return nil
				})
			}}, Observe: func(ctx context.Context, c ownership.Credential) (ownership.Observation, error) {
			return taskObservation(ctx, db, c.Key)
		}})
	require.NoError(t, err)
	t.Cleanup(func() { once.Do(func() { close(hold) }); require.NoError(t, runtime.Close(ctx)) })
	select {
	case <-aEntered:
	case <-time.After(time.Second):
		t.Fatal("真实A事务未进入")
	}
	require.Eventually(t, func() bool {
		var row ownershipSQLTask
		return db.First(&row, tasks[3].ID).Error == nil && row.Writes == 1
	}, 700*time.Millisecond, time.Millisecond, "B2须在A1共享SQL事务退出前真实提交")
	require.EqualValues(t, 1, aQueries.Load(), "A2不能进入真实头查询")
	require.Eventually(t, func() bool { return dCalls.Load() == 1 }, time.Second, time.Millisecond)
	var kept []ownershipSQLTask
	require.NoError(t, db.Where("id IN ?", []uint{tasks[4].ID, tasks[5].ID, tasks[6].ID, tasks[7].ID}).Order("id").Find(&kept).Error)
	require.Equal(t, "paused", kept[0].State)
	require.Equal(t, "pending", kept[1].State)
	require.Zero(t, kept[1].Writes)
	require.Equal(t, "unknown", kept[2].State)
	require.EqualValues(t, 1, kept[2].Writes)
	require.Equal(t, "pending", kept[3].State)
	require.Zero(t, kept[3].Writes)
	require.EqualValues(t, 2, kept[2].Attempts)
	require.EqualValues(t, 3, kept[3].Attempts)

	require.Eventually(t, func() bool {
		var row ownershipSQLTask
		return db.First(&row, tasks[8].ID).Error == nil && row.State == "paused"
	}, time.Second, time.Millisecond)
	var failedRows []ownershipSQLTask
	require.NoError(t, db.Where("id IN ?", []uint{tasks[8].ID, tasks[9].ID}).Order("ordinal").Find(&failedRows).Error)
	require.EqualValues(t, 0, failedRows[0].Writes, "实际事务回滚不能变成已确认")
	require.EqualValues(t, 3, failedRows[0].Attempts)
	require.Equal(t, "pending", failedRows[1].State)
	require.Zero(t, failedRows[1].Writes)
	require.EqualValues(t, 3, failedRows[1].Attempts)
	once.Do(func() { close(hold) })
	require.Eventually(t, func() bool {
		var row ownershipSQLTask
		return db.First(&row, tasks[1].ID).Error == nil && row.Writes == 1
	}, time.Second, time.Millisecond)
	// 应用核对原事实后持久决策为resolved；只解除原前序，不重放未知业务写入。
	require.NoError(t, db.Model(&ownershipSQLTask{}).Where("id = ?", tasks[6].ID).Update("state", "resolved").Error)
	// 后继D2沿原正式执行路径，不能再走D1未知模拟。
	require.NoError(t, runtime.Close(ctx))
	freshBudget, err := ownership.NewGroupWorkBudget("owner", ownership.GroupWorkBudgetOptions{Workers: 2, Prefetch: 2})
	require.NoError(t, err)
	defer freshBudget.Close(ctx)
	// 同进程重开仅用于API生命周期回归；独立PID恢复另测，不能相互替代。
	again, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "sql-next", MaxOwned: 1,
		Activate: func(ctx context.Context, c ownership.Credential) error {
			return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return ownership.ActivateFence(mysqlFenceTransaction{tx}, c) })
		},
		GroupWork: &ownership.GroupWorkOptions{Budget: freshBudget, NextGroups: func(ctx context.Context, c ownership.Credential, q ownership.GroupWorkRequest) ([]string, error) {
			return sqlReadyGroups(ctx, db, c, q)
		}, StepGroup: func(ctx context.Context, c ownership.Credential, g string) (bool, error) {
			return sqlGroupStep(ctx, db, c, g, nil)
		}}, Observe: func(ctx context.Context, c ownership.Credential) (ownership.Observation, error) {
			return taskObservation(ctx, db, c.Key)
		}})
	require.NoError(t, err)
	defer again.Close(ctx)
	require.Eventually(t, func() bool {
		var row ownershipSQLTask
		return db.First(&row, tasks[7].ID).Error == nil && row.Writes == 1
	}, time.Second, time.Millisecond)
	var original ownershipSQLTask
	require.NoError(t, db.First(&original, tasks[6].ID).Error)
	require.EqualValues(t, 1, original.Writes)
	require.EqualValues(t, 2, original.Attempts)
}
