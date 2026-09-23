package oltp

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"testing"

	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type persistenceErrorRecord struct {
	ID uint `gorm:"primaryKey"`
}

type persistenceTestDriver struct{}

func (persistenceTestDriver) Open(string) (driver.Conn, error) {
	return &persistenceTestConn{}, nil
}

type persistenceTestConn struct{}

func (*persistenceTestConn) Prepare(string) (driver.Stmt, error) { return &persistenceTestStmt{}, nil }
func (*persistenceTestConn) Close() error                        { return nil }
func (*persistenceTestConn) Begin() (driver.Tx, error)           { return persistenceTestTx{}, nil }
func (*persistenceTestConn) Ping(context.Context) error          { return nil }

type persistenceTestStmt struct{}

func (*persistenceTestStmt) Close() error  { return nil }
func (*persistenceTestStmt) NumInput() int { return -1 }
func (*persistenceTestStmt) Exec([]driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (*persistenceTestStmt) Query([]driver.Value) (driver.Rows, error) {
	return &persistenceTestRows{}, nil
}

type persistenceTestRows struct{}

func (*persistenceTestRows) Columns() []string         { return []string{"result"} }
func (*persistenceTestRows) Close() error              { return nil }
func (*persistenceTestRows) Next([]driver.Value) error { return io.EOF }

type persistenceTestTx struct{}

func (persistenceTestTx) Commit() error   { return nil }
func (persistenceTestTx) Rollback() error { return nil }

var registerPersistenceTestDriver sync.Once

func mysqlAdapterReturningCreateError(t *testing.T, cause error) *MySQL {
	t.Helper()
	registerPersistenceTestDriver.Do(func() {
		sql.Register("core-persistence-error-contract", persistenceTestDriver{})
	})
	sqlDB, err := sql.Open("core-persistence-error-contract", "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DisableAutomaticPing: true})
	require.NoError(t, err)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:return_error", func(tx *gorm.DB) {
		tx.AddError(cause)
	}))
	return &MySQL{
		Name:   "error_contract",
		db:     db,
		tables: make(map[string]*TableMaster),
		config: &Config{Database: "error_contract"},
	}
}

func assertSafePersistenceContract(t *testing.T, err error, kind servertypes.ErrorKind, code, status int, message string, cause error) {
	t.Helper()
	require.Error(t, err)
	var publicErr *servertypes.PublicError
	require.ErrorAs(t, err, &publicErr)
	contract := servertypes.ResolvePublicError(err)
	require.Equal(t, kind, contract.Kind)
	require.Equal(t, code, contract.Code)
	require.Equal(t, status, contract.HTTPStatus)
	require.Equal(t, message, contract.Message)
	require.NotContains(t, contract.Message, "futures_specs")
	require.NotContains(t, contract.Message, "idx_futures_specs_market_id")
	require.ErrorIs(t, err, cause)
}

// TestMySQLInsertClassifiesUniqueConstraintErrors 验证 GORM 翻译错误与 MySQL 稳定错误类型统一映射为冲突。
func TestMySQLInsertClassifiesUniqueConstraintErrors(t *testing.T) {
	tests := []struct {
		name  string
		cause error
	}{
		{name: "gorm", cause: gorm.ErrDuplicatedKey},
		{name: "mysql", cause: &mysqlDriver.MySQLError{Number: 1062, Message: "Duplicate entry '851993083058053' for key 'futures_specs.idx_futures_specs_market_id'"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := mysqlAdapterReturningCreateError(t, tt.cause)
			err := adapter.Insert(&persistenceErrorRecord{ID: 1})
			assertSafePersistenceContract(t, err, servertypes.ErrorKindConflict, servertypes.PublicCodeConflict, 409, "record already exists", tt.cause)
		})
	}
}

// TestMySQLInsertClassifiesUnknownDatabaseErrorAsInternal 验证未知持久化错误 fail closed 且保留原始 cause。
func TestMySQLInsertClassifiesUnknownDatabaseErrorAsInternal(t *testing.T) {
	cause := errors.New("database host=10.0.0.8 password=private")
	adapter := mysqlAdapterReturningCreateError(t, cause)

	err := adapter.Insert(&persistenceErrorRecord{ID: 1})
	assertSafePersistenceContract(t, err, servertypes.ErrorKindInternal, servertypes.PublicCodeInternal, 500, "internal server error", cause)
}
