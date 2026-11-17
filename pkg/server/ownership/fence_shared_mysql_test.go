// 本文件以真实MySQL共享当前读验证新显式fence合同；旧排他Check/Activate保持。
package ownership_test

import (
	"database/sql"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"sync"
	"testing"
	"time"
)

type mysqlSharedFenceTransaction struct{ db *gorm.DB }

func (tx mysqlSharedFenceTransaction) LockFenceShared(scope, key string) (ownership.FenceState, error) {
	var row mysqlFenceFixture
	err := tx.db.Clauses(clause.Locking{Strength: "SHARE"}).Where("scope = ? AND key_name = ?", scope, key).Take(&row).Error
	return ownership.FenceState{Scope: row.Scope, Key: row.KeyName, Epoch: row.Epoch, Token: row.Token}, err
}

// TestSharedFenceMySQLReadersCoexistAndActivateWaitsAll两个不同组的S事务并存，新X必须等二者真实提交。
func TestSharedFenceMySQLReadersCoexistAndActivateWaitsAll(t *testing.T) {
	db := mysqlFenceDatabase(t, &ownershipSQLTask{}).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	c := ownership.Credential{Scope: "shared", Key: "bucket", Service: "fixture", InstanceID: "old", SessionID: "boot", Epoch: 1, Token: "synthetic-shared-old"}
	require.NoError(t, db.Create(&mysqlFenceFixture{Scope: c.Scope, KeyName: c.Key, Epoch: c.Epoch, Token: c.Token}).Error)
	rows := []ownershipSQLTask{{OrderKey: "A"}, {OrderKey: "B"}}
	require.NoError(t, db.Create(&rows).Error)
	first := db.Begin()
	require.NoError(t, first.Error)
	defer first.Rollback()
	require.NoError(t, ownership.CheckFenceShared(mysqlSharedFenceTransaction{first}, c))
	require.NoError(t, first.Model(&ownershipSQLTask{}).Where("id = ?", rows[0].ID).UpdateColumn("writes", gorm.Expr("writes+1")).Error)
	secondLocked := make(chan struct{})
	release := make(chan struct{})
	secondDone := make(chan error, 1)
	activated := make(chan error, 1)
	var once sync.Once
	defer once.Do(func() { close(release) })
	go func() {
		secondDone <- db.Transaction(func(tx *gorm.DB) error {
			if err := ownership.CheckFenceShared(mysqlSharedFenceTransaction{tx}, c); err != nil {
				return err
			}
			if err := tx.Model(&ownershipSQLTask{}).Where("id = ?", rows[1].ID).UpdateColumn("writes", gorm.Expr("writes+1")).Error; err != nil {
				return err
			}
			close(secondLocked)
			<-release
			return nil
		})
	}()
	select {
	case <-secondLocked:
	case err := <-secondDone:
		t.Fatalf("第二共享事务失败 %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("独立组共享Check仍被另一共享事务阻挡")
	}
	next := c
	next.Epoch = 2
	next.InstanceID = "new"
	next.SessionID = "new-boot"
	next.Token = "synthetic-shared-new"
	go func() {
		activated <- db.Transaction(func(tx *gorm.DB) error { return ownership.ActivateFence(mysqlFenceTransaction{tx}, next) })
	}()
	select {
	case err := <-activated:
		t.Fatalf("旧共享事务尚未退出却激活 %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, first.Commit().Error)
	select {
	case err := <-activated:
		t.Fatalf("第二共享事务尚未退出却激活 %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	require.NoError(t, <-secondDone)
	require.NoError(t, <-activated)
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return ownership.CheckFenceShared(mysqlSharedFenceTransaction{tx}, c) }), ownership.ErrFenced)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return ownership.CheckFenceShared(mysqlSharedFenceTransaction{tx}, next) }))
	wrong := next
	wrong.Token = "synthetic-wrong"
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return ownership.CheckFenceShared(mysqlSharedFenceTransaction{tx}, wrong) }), ownership.ErrFenced)
	var committed []ownershipSQLTask
	require.NoError(t, db.Order("id").Find(&committed).Error)
	require.EqualValues(t, 1, committed[0].Writes)
	require.EqualValues(t, 1, committed[1].Writes)
}

// TestSharedFenceMySQLUsesCurrentRead旧快照不能使旧epoch通过共享锁定当前读；原排他路径仍等待。
func TestSharedFenceMySQLUsesCurrentRead(t *testing.T) {
	db := mysqlFenceDatabase(t).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	c := ownership.Credential{Scope: "current-read", Key: "bucket", Service: "fixture", InstanceID: "old", SessionID: "boot", Epoch: 1, Token: "synthetic-old"}
	require.NoError(t, db.Create(&mysqlFenceFixture{Scope: c.Scope, KeyName: c.Key, Epoch: c.Epoch, Token: c.Token}).Error)
	snapshot := db.Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	require.NoError(t, snapshot.Error)
	defer snapshot.Rollback()
	var row mysqlFenceFixture
	require.NoError(t, snapshot.Where("scope = ? AND key_name = ?", c.Scope, c.Key).Take(&row).Error)
	require.EqualValues(t, 1, row.Epoch)
	next := c
	next.Epoch = 2
	next.Token = "synthetic-new"
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return ownership.ActivateFence(mysqlFenceTransaction{tx}, next) }))
	require.ErrorIs(t, ownership.CheckFenceShared(mysqlSharedFenceTransaction{snapshot}, c), ownership.ErrFenced)
	require.NoError(t, snapshot.Rollback().Error)
	first := db.Begin()
	require.NoError(t, ownership.CheckFence(mysqlFenceTransaction{first}, next))
	blocked := make(chan error, 1)
	go func() {
		blocked <- db.Transaction(func(tx *gorm.DB) error { return ownership.CheckFence(mysqlFenceTransaction{tx}, next) })
	}()
	defer first.Rollback()
	select {
	case err := <-blocked:
		t.Fatalf("旧排他路径被静默降锁 %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, first.Commit().Error)
	require.NoError(t, <-blocked)
}

// TestSharedFenceRejectsInvalidCredential不发SQL时也保持空接口/无效凭据失败关闭。
func TestSharedFenceRejectsInvalidCredential(t *testing.T) {
	require.ErrorIs(t, ownership.CheckFenceShared(nil, ownership.Credential{}), ownership.ErrUnknown)
}
