package test

import (
	"strings"
	"testing"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/require"
)

func TestSqliteRawReturnsCurrentResultError(t *testing.T) {
	useTemporaryDatabasePath(t)
	adapter := oltp.NewSqlite()
	var result []SQLiteTestUser

	err := adapter.Raw("SELECT * FROM table_that_does_not_exist", &result)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "table_that_does_not_exist"), "应返回本次 Raw/Scan 的错误，实际: %v", err)
}

func TestSqliteExecReturnsCurrentResultError(t *testing.T) {
	useTemporaryDatabasePath(t)
	adapter := oltp.NewSqlite()

	err := adapter.Exec("INVALID SQL", &SQLiteTestUser{})
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "unsupported type", "应返回本次 Exec 的参数错误")
}

func useTemporaryDatabasePath(t *testing.T) {
	t.Helper()
	previous := utils.TESTPATH
	utils.TESTPATH = t.TempDir()
	t.Cleanup(func() { utils.TESTPATH = previous })
}
