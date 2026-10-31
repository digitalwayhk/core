//go:build integration

// 本文件验证 PostgreSQL 权限、加性结构初始化和模型分库的真实边界。
package oltp

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type pgSchemaBefore struct {
	ID     uint   `gorm:"primaryKey"`
	Value  string `gorm:"type:varchar(64)"`
	Legacy string
}

func (pgSchemaBefore) TableName() string { return "pg_additive_boundary" }

type pgSchemaAfter struct {
	ID    uint   `gorm:"primaryKey"`
	Value string `gorm:"type:varchar(8)"`
	Added string `gorm:"index"`
}

func (pgSchemaAfter) TableName() string { return "pg_additive_boundary" }

type pgIndexBefore struct {
	ID    uint `gorm:"primaryKey"`
	Value string
}

func (pgIndexBefore) TableName() string { return "pg_index_boundary" }

type pgIndexAfter struct {
	ID    uint   `gorm:"primaryKey"`
	Value string `gorm:"uniqueIndex:pg_boundary_unique"`
}

func (pgIndexAfter) TableName() string { return "pg_index_boundary" }

// TestPostgresIntegration_AdditiveSchema 保留旧类型和列，补列索引；唯一索引失败必须阻止启动。
func TestPostgresIntegration_AdditiveSchema(t *testing.T) {
	c := postgresIntegrationConfig(t)
	c.Database = fmt.Sprintf("core_pg_schema_%d", time.Now().UnixNano())
	p := NewPostgres(c)
	t.Setenv("DB_FORCE_MIGRATE", "true")
	require.NoError(t, p.HasTable(&pgSchemaBefore{}))
	require.NoError(t, p.Insert(&pgSchemaBefore{Value: "超过八字节仍须保留的旧值", Legacy: "保留"}))
	require.NoError(t, p.HasTable(&pgSchemaAfter{}))
	db, err := p.GetDB()
	require.NoError(t, err)
	require.True(t, db.Migrator().HasColumn(&pgSchemaAfter{}, "legacy"))
	require.True(t, db.Migrator().HasColumn(&pgSchemaAfter{}, "added"))
	require.True(t, db.Migrator().HasIndex(&pgSchemaAfter{}, "idx_pg_additive_boundary_added"))
	var length int
	require.NoError(t, db.Raw("SELECT character_maximum_length FROM information_schema.columns WHERE table_name = ? AND column_name = ?", "pg_additive_boundary", "value").Scan(&length).Error)
	require.Equal(t, 64, length)
	require.NoError(t, p.HasTable(&pgIndexBefore{}))
	require.NoError(t, p.Insert(&[]pgIndexBefore{{Value: "duplicate"}, {Value: "duplicate"}}))
	for i := 0; i < 2; i++ {
		err = p.HasTable(&pgIndexAfter{})
		var pg *pgconn.PgError
		require.True(t, errors.As(err, &pg), "%v", err)
		require.Equal(t, "23505", pg.Code)
	}
}

type pgRoutedRecord struct {
	ID       uint   `gorm:"primaryKey"`
	Database string `gorm:"-"`
}

type pgParentRecord struct {
	ID       uint            `gorm:"primaryKey"`
	Children []pgChildRecord `gorm:"foreignKey:ParentID"`
}
type pgChildRecord struct {
	ID       uint `gorm:"primaryKey"`
	ParentID uint
	One      string
	Two      string
	Three    string
}

type pgTaggedRecord struct {
	ID   uint          `gorm:"primaryKey"`
	Tags []pgTagRecord `gorm:"many2many:pg_record_tags"`
}
type pgTagRecord struct {
	ID uint `gorm:"primaryKey"`
}

// TestPostgresIntegration_AssociationAndReadiness 验证启动初始化关联表，迁移缓存驱逐不丢失事务就绪事实。
func TestPostgresIntegration_AssociationAndReadiness(t *testing.T) {
	c := postgresIntegrationConfig(t)
	c.Database = fmt.Sprintf("core_pg_association_%d", time.Now().UnixNano())
	p := NewPostgres(c)
	require.NoError(t, p.HasTable(&pgParentRecord{}))
	require.NoError(t, p.Insert(&pgParentRecord{ID: 1, Children: []pgChildRecord{{ID: 1}}}))
	db, err := p.GetDB()
	require.NoError(t, err)
	key, err := p.tableKey(db, c.Database, &pgParentRecord{})
	require.NoError(t, err)
	tableCache.Delete(key)
	tx := p.Clone()
	require.NoError(t, tx.Transaction())
	require.NoError(t, tx.Insert(&pgParentRecord{ID: 2}))
	require.NoError(t, tx.Commit())
	require.NoError(t, p.HasTable(&pgParentRecord{}))
	_, cached := tableCache.Load(key)
	require.True(t, cached)
	second := NewPostgres(c)
	require.NoError(t, second.HasTable(&pgParentRecord{}))
	childKey, err := p.tableKey(db, c.Database, &pgChildRecord{})
	require.NoError(t, err)
	tableCache.Delete(childKey)
	tx = second.Clone()
	require.NoError(t, tx.Transaction())
	require.NoError(t, tx.Insert(&pgChildRecord{ID: 3, ParentID: 2}))
	require.NoError(t, tx.Commit())
	require.NoError(t, p.HasTable(&pgTaggedRecord{}))
	require.NoError(t, p.Insert(&pgTaggedRecord{ID: 1, Tags: []pgTagRecord{{ID: 1}}}))
	children := make([]pgChildRecord, 14000)
	for i := range children {
		children[i].ID = uint(i + 100)
	}
	require.NoError(t, p.Insert(&pgParentRecord{ID: 4, Children: children}))
}

// TestPostgresIntegration_WriteFailure 验证写失败不重放，断开的活动事务不换连接。
func TestPostgresIntegration_WriteFailure(t *testing.T) {
	c := postgresIntegrationConfig(t)
	c.Database = fmt.Sprintf("core_pg_write_failure_%d", time.Now().UnixNano())
	p := NewPostgres(c)
	require.NoError(t, p.HasTable(&pgContractRecord{}))
	db, err := p.GetDB()
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, pool.Close())
	require.Error(t, p.Insert(&pgContractRecord{ID: 1, Hashcode: "must-not-replay"}))
	var rows []pgContractRecord
	query := &types.SearchItem{Model: &pgContractRecord{}, Size: 10}
	require.NoError(t, p.Load(query, &rows))
	require.Empty(t, rows)
	tx := p.Clone().(*Postgres)
	require.NoError(t, tx.Transaction())
	require.NoError(t, tx.Insert(&pgContractRecord{ID: 2, Hashcode: "before-disconnect"}))
	txDB := tx.GetRunDB().(*gorm.DB)
	var pid int
	require.NoError(t, txDB.Raw("SELECT pg_backend_pid()").Scan(&pid).Error)
	db, err = p.GetDB()
	require.NoError(t, err)
	require.NoError(t, db.Exec("SELECT pg_terminate_backend(?)", pid).Error)
	require.Error(t, tx.Insert(&pgContractRecord{ID: 3, Hashcode: "after-disconnect"}))
	require.Error(t, tx.Load(query, &rows))
	require.Same(t, txDB, tx.GetRunDB())
	require.Error(t, tx.Commit())
	rows = nil
	require.NoError(t, p.Load(query, &rows))
	require.Empty(t, rows)
}

func (*pgRoutedRecord) TableName() string         { return "pg_routed_record" }
func (r *pgRoutedRecord) GetRemoteDBName() string { return r.Database }
func (*pgRoutedRecord) GetLocalDBName() string    { return "" }

// TestPostgresIntegration_DynamicRouting 验证模型实例分库、批量分库及事务拒绝跨库。
func TestPostgresIntegration_DynamicRouting(t *testing.T) {
	c := postgresIntegrationConfig(t)
	c.Database = ""
	p := NewPostgres(c)
	prefix := fmt.Sprintf("core_pg_route_%d", time.Now().UnixNano())
	first := &pgRoutedRecord{Database: prefix + "_a"}
	second := &pgRoutedRecord{Database: prefix + "_b"}
	require.NoError(t, p.HasTable(first))
	require.NoError(t, p.HasTable(second))
	require.NotEqual(t, p.GetSyncPoolKey(first), p.GetSyncPoolKey(second))
	require.NoError(t, p.Insert(&[]pgRoutedRecord{{ID: 1, Database: first.Database}}))
	tx := p.Clone()
	require.NoError(t, tx.Transaction())
	require.NoError(t, tx.Insert(&pgRoutedRecord{ID: 2, Database: first.Database}))
	require.ErrorContains(t, tx.Insert(&pgRoutedRecord{ID: 3, Database: second.Database}), "another database")
	require.NoError(t, tx.Rollback())
	require.Error(t, p.HasTable(&pgRoutedRecord{}))
	require.ErrorContains(t, p.Insert(&[]pgRoutedRecord{{ID: 4, Database: first.Database}, {ID: 5, Database: second.Database}}), "multiple databases")
	var missing *pgRoutedRecord
	require.Error(t, p.HasTable(missing))
}

// TestPostgresIntegration_LimitedRole 验证已建库无需CREATEDB，缺库且无权限时明确失败。
func TestPostgresIntegration_LimitedRole(t *testing.T) {
	c := postgresIntegrationConfig(t)
	admin := NewPostgres(c)
	db, err := admin.GetDB()
	require.NoError(t, err)
	name := fmt.Sprintf("core_pg_limited_%d", time.Now().UnixNano())
	require.NoError(t, db.Exec("CREATE ROLE "+quoteIdent(DialectPostgres, name)+" LOGIN PASSWORD 'core_limited_test' NOCREATEDB").Error)
	require.NoError(t, db.Exec("CREATE DATABASE "+quoteIdent(DialectPostgres, name)+" OWNER "+quoteIdent(DialectPostgres, name)).Error)
	c.Database, c.Username, c.Password = name, name, "core_limited_test"
	p := NewPostgres(c)
	t.Cleanup(func() {
		connManager.Remove(storageIdentity(c, name))
		require.NoError(t, db.Exec("DROP DATABASE "+quoteIdent(DialectPostgres, name)).Error)
		require.NoError(t, db.Exec("DROP ROLE "+quoteIdent(DialectPostgres, name)).Error)
	})
	require.NoError(t, p.HasTable(&pgContractRecord{}))
	require.NoError(t, p.Insert(&pgContractRecord{ID: 1, Hashcode: "limited"}))
	c.Database = name + "_missing"
	err = NewPostgres(c).HasTable(&pgContractRecord{})
	var pg *pgconn.PgError
	require.True(t, errors.As(err, &pg), "%v", err)
	require.Equal(t, "42501", pg.Code)
}
