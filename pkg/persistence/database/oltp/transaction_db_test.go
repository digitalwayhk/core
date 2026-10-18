// 本文件验证事务 SQL 访问不会退回基础连接，且提交/回滚后不能继续获取旧事务。
package oltp

import (
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type transactionProbe struct {
	ID    uint `gorm:"primaryKey"`
	Value string
}

// TestTransactionDBPreservesRollback 验证 SQLite/MySQL action 返回的是事务连接，写入可被回滚。
func TestTransactionDBPreservesRollback(t *testing.T) {
	base := NewFixedSqlite(fmt.Sprintf("tx_connection_%d", time.Now().UnixNano()))
	t.Cleanup(func() { require.NoError(t, base.DeleteDB()) })
	var rows []*transactionProbe
	require.NoError(t, base.Load(&types.SearchItem{Model: &transactionProbe{}, Page: 1, Size: 1, SkipCount: true}, &rows))
	db := base.GetRunDB().(*gorm.DB)
	for _, driver := range []string{"sqlite", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			tx := db.Begin()
			require.NoError(t, tx.Error)
			var action types.ITransactionDB
			if driver == "sqlite" {
				action = &Sqlite{db: db, tx: tx, isTansaction: true}
			} else {
				action = &MySQL{db: db, tx: tx, isTansaction: true}
			}
			running, err := action.GetTransactionDB()
			require.NoError(t, err)
			require.Same(t, tx, running)
			require.NoError(t, running.Create(&transactionProbe{Value: driver}).Error)
			require.NoError(t, tx.Rollback().Error)
			var count int64
			require.NoError(t, db.Model(&transactionProbe{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

// TestTransactionDBRejectsUnstartedTransaction 验证懒事务未绑定连接和普通连接均被拒绝。
func TestTransactionDBRejectsUnstartedTransaction(t *testing.T) {
	for _, action := range []types.ITransactionDB{NewSqlite(), NewMySQL(&Config{})} {
		_, err := action.GetTransactionDB()
		require.Error(t, err)
	}
}
