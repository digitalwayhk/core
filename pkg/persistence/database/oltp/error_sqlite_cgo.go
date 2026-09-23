//go:build cgo

package oltp

import (
	"errors"

	"github.com/mattn/go-sqlite3"
)

func isSQLiteUniqueConstraintError(err error) bool {
	var sqliteErr sqlite3.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	return sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique ||
		sqliteErr.ExtendedCode == sqlite3.ErrConstraintPrimaryKey
}
