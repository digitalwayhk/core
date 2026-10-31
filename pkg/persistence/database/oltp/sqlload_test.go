// 本文件验证原生投影查询不要求ID列，并保留显式排序及参数绑定。
package oltp

import (
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
)

type sqlProjection struct{ Value int }

func (*sqlProjection) SearchSQL() string { return "SELECT 42 AS value UNION ALL SELECT 7 AS value" }

// TestSQLProjectionWithoutID 验证无ID投影默认可查询，并支持筛选排序分页。
func TestSQLProjectionWithoutID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	model := &sqlProjection{}
	item := &types.SearchItem{Model: model, Page: 1, Size: 10}
	var rows []sqlProjection
	require.NoError(t, sqlload(model, db, item, &rows))
	require.Len(t, rows, 2)
	item.AddSortN("Value", false)
	item.AddWhereNS("Value", ">", 10)
	require.NoError(t, sqlload(model, db, item, &rows))
	require.Equal(t, []sqlProjection{{Value: 42}}, rows)
}
