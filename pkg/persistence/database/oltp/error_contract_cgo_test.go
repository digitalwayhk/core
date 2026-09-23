//go:build cgo

package oltp

import (
	"errors"
	"testing"

	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

type uniquePersistenceErrorRecord struct {
	ID    uint   `gorm:"primaryKey"`
	Value string `gorm:"uniqueIndex"`
}

func (*uniquePersistenceErrorRecord) GetLocalDBName() string {
	return "core_public_error_sqlite_test"
}

func (*uniquePersistenceErrorRecord) GetRemoteDBName() string {
	return "core_public_error_sqlite_test"
}

// TestSQLiteInsertClassifiesUniqueConstraintAndPreservesDriverError 验证 SQLite 冲突保留可 errors.As 的驱动错误。
func TestSQLiteInsertClassifiesUniqueConstraintAndPreservesDriverError(t *testing.T) {
	adapter := NewSqlite()
	t.Cleanup(func() { require.NoError(t, adapter.DeleteDB()) })
	require.NoError(t, adapter.Insert(&uniquePersistenceErrorRecord{ID: 1, Value: "same"}))

	err := adapter.Insert(&uniquePersistenceErrorRecord{ID: 2, Value: "same"})
	require.Error(t, err)
	contract := servertypes.ResolvePublicError(err)
	require.Equal(t, servertypes.ErrorKindConflict, contract.Kind)
	require.Equal(t, servertypes.PublicCodeConflict, contract.Code)
	require.Equal(t, 409, contract.HTTPStatus)
	require.Equal(t, "record already exists", contract.Message)
	var sqliteErr sqlite3.Error
	require.True(t, errors.As(err, &sqliteErr))
	require.Equal(t, sqlite3.ErrConstraintUnique, sqliteErr.ExtendedCode)
}
