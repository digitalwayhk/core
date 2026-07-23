package oltp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSqliteCloneStartsWithoutTransactionState 验证克隆实例不继承源实例的事务状态。
func TestSqliteCloneStartsWithoutTransactionState(t *testing.T) {
	source := NewSqlite()
	require.NoError(t, source.Transaction())

	clone, ok := source.Clone().(*Sqlite)
	require.True(t, ok)
	assert.False(t, clone.isTansaction)
	assert.Nil(t, clone.tx)
}
