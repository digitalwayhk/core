// 本文件验证可选查询上下文覆盖普通读取、免COUNT读取与统计，且不进入JSON。
package oltp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
)

// TestSQLQueryContext 验证取消对SQL读取各分支生效，nil保持既有读取行为。
func TestSQLQueryContext(t *testing.T) {
	action := NewFixedSqlite(fmt.Sprintf("query_context_%d", time.Now().UnixNano()))
	t.Cleanup(func() { require.NoError(t, action.DeleteDB()) })
	var rows []*transactionProbe
	require.NoError(t, action.Load(&types.SearchItem{Model: &transactionProbe{}, Size: 1}, &rows))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, mode := range []string{"count_and_read", "skip_count", "statistical"} {
		t.Run(mode, func(t *testing.T) {
			query := &types.SearchItem{Context: ctx, Model: &transactionProbe{}, Page: 1, Size: 1, SkipCount: mode == "skip_count", IsStatistical: mode == "statistical", Statistical: "count"}
			var result interface{} = &rows
			if query.IsStatistical {
				result = &struct{ Total int64 }{}
			}
			require.ErrorIs(t, action.Load(query, result), context.Canceled)
			encoded, err := json.Marshal(query)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "Context")
		})
	}
	require.NoError(t, action.Load(&types.SearchItem{Model: &transactionProbe{}, Size: 1}, &rows))
}
