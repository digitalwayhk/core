// 本文件用独占真实MySQL事务验证fence；仅通过Core数据访问初始化表，不用迁移脚本。
package ownership_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"os"
	"testing"
	"time"
)

// mysqlFenceFixture 是测试专用技术记录，不是应用业务模型或默认业务状态机。
type mysqlFenceFixture struct {
	ID      uint   `gorm:"primaryKey"`
	Scope   string `gorm:"size:128;uniqueIndex:scope_key"`
	KeyName string `gorm:"size:128;uniqueIndex:scope_key"`
	Epoch   int64
	Token   string `gorm:"size:128"`
	Writes  int64
	LastID  string
}

type mysqlFenceTransaction struct{ db *gorm.DB }

func (tx mysqlFenceTransaction) LockFence(scope, key string) (ownership.FenceState, error) {
	var row mysqlFenceFixture
	err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope = ? AND key_name = ?", scope, key).Take(&row).Error
	return ownership.FenceState{Scope: row.Scope, Key: row.KeyName, Epoch: row.Epoch, Token: row.Token}, err
}
func (tx mysqlFenceTransaction) SaveFence(state ownership.FenceState) error {
	return tx.db.Model(&mysqlFenceFixture{}).Where("scope = ? AND key_name = ?", state.Scope, state.Key).Updates(map[string]any{"epoch": state.Epoch, "token": state.Token}).Error
}

func mysqlFenceDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	file := os.Getenv("CORE_TEST_MYSQL_OWNERSHIP_CONFIG_FILE")
	if file == "" {
		t.Skip("NOT_RUN: CORE_TEST_MYSQL_OWNERSHIP_CONFIG_FILE")
	}
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	var cfg oltp.Config
	require.NoError(t, json.Unmarshal(raw, &cfg))
	cfg.Database = fmt.Sprintf("core_fence_%d", time.Now().UnixNano())
	cfg.IsLog = false
	action := oltp.NewMySQL(&cfg)
	var rows []*mysqlFenceFixture
	// 启动组合根：框架负责自动建库/表/索引；事务和handler不承担DDL。
	require.NoError(t, action.Load(&persistencetypes.SearchItem{Page: 1, Size: 1, Model: &mysqlFenceFixture{}}, &rows))
	db, err := action.GetDB()
	require.NoError(t, err)
	return db
}

func mysqlFenceBroker(t *testing.T) (*mq.RedisStreamProvider, ownership.Store, ownership.Manifest) {
	t.Helper()
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR")
	}
	m := ownership.Manifest{Scope: fmt.Sprintf("mysql-fence-%d", time.Now().UnixNano()), Service: "fixture", Group: "fixture", Selector: "key-v1", Keys: []string{"A"}, Subjects: []string{"one"}}.Normalize()
	p := mq.NewRedisStreamProviderWithConfig(config.RedisStreamMQConfig{Addr: addr, Prefix: m.Scope, CommandPoolSize: 4, BlockingReadPoolSize: 4})
	require.NoError(t, p.Connect(context.Background()))
	t.Cleanup(func() { require.NoError(t, p.Close()) })
	require.NoError(t, p.ProvisionOwnership(context.Background(), m, 0))
	store, err := p.OwnershipStore(context.Background())
	require.NoError(t, err)
	return p, store, m
}

func TestMySQLFenceWaitsForExpiredOwnersRealSQLTransaction(t *testing.T) {
	db := mysqlFenceDatabase(t)
	_, store, m := mysqlFenceBroker(t)
	ctx := context.Background()
	require.NoError(t, db.Create(&mysqlFenceFixture{Scope: m.Scope, KeyName: "A"}).Error)
	old, err := store.Acquire(ctx, ownership.Credential{Scope: m.Scope, Key: "A", Service: m.Service, InstanceID: "old", SessionID: "old-boot"}, m.Timing.Lease)
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return ownership.ActivateFence(mysqlFenceTransaction{tx}, old) }))
	require.NoError(t, store.Ready(ctx, old))
	oldSQL := db.Begin()
	require.NoError(t, oldSQL.Error)
	defer oldSQL.Rollback()
	require.NoError(t, ownership.CheckFence(mysqlFenceTransaction{oldSQL}, old))
	// 原生默认15秒租约：不续租但保留SQL事务和行锁，不能用租约到期代替SQL退出。
	deadline := time.Now().Add(m.Timing.Lease + 2*time.Second)
	var next ownership.Credential
	for time.Now().Before(deadline) {
		next, err = store.Acquire(ctx, ownership.Credential{Scope: m.Scope, Key: "A", Service: m.Service, InstanceID: "new", SessionID: "new-boot"}, m.Timing.Lease)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.NoError(t, err)
	require.Greater(t, next.Epoch, old.Epoch)
	activated := make(chan error, 1)
	go func() {
		err := db.Transaction(func(tx *gorm.DB) error { return ownership.ActivateFence(mysqlFenceTransaction{tx}, next) })
		if err == nil {
			err = store.Ready(ctx, next)
		}
		activated <- err
	}()
	select {
	case err := <-activated:
		t.Fatalf("旧SQL尚持锁，新激活不应完成：%v", err)
	case <-time.After(150 * time.Millisecond):
	}
	_, err = store.Resolve(ctx, m.Scope, "A")
	require.ErrorIs(t, err, ownership.ErrNotReady)
	require.NoError(t, oldSQL.Model(&mysqlFenceFixture{}).Where("scope = ? AND key_name = ?", m.Scope, "A").UpdateColumn("writes", gorm.Expr("writes + 1")).Error)
	require.NoError(t, oldSQL.Commit().Error)
	require.NoError(t, <-activated)
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := ownership.CheckFence(mysqlFenceTransaction{tx}, old); err != nil {
			return err
		}
		return tx.Model(&mysqlFenceFixture{}).Where("scope = ? AND key_name = ?", m.Scope, "A").UpdateColumn("writes", gorm.Expr("writes + 100")).Error
	})
	require.ErrorIs(t, err, ownership.ErrFenced)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := ownership.CheckFence(mysqlFenceTransaction{tx}, next); err != nil {
			return err
		}
		return tx.Model(&mysqlFenceFixture{}).Where("scope = ? AND key_name = ?", m.Scope, "A").UpdateColumn("writes", gorm.Expr("writes + 1")).Error
	}))
	var final mysqlFenceFixture
	require.NoError(t, db.Where("scope = ? AND key_name = ?", m.Scope, "A").Take(&final).Error)
	require.EqualValues(t, 2, final.Writes)
	require.Equal(t, next.Epoch, final.Epoch)
}
