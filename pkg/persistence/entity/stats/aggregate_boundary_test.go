// 本文件验证统计刷新只依赖中性聚合能力，不要求 GORM 或数据库类型。
package stats

import (
	"context"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type aggregateOnlyAction struct {
	types.IDataAction
	query types.AggregateQuery
}

func (a *aggregateOnlyAction) Aggregate(_ context.Context, q types.AggregateQuery) ([]types.AggregateRow, error) {
	a.query = q
	return []types.AggregateRow{{Bucket: "2026-01-01", Metrics: map[string]string{"row_count": "2"}}}, nil
}

// TestStatsWithoutDatabaseHandle 验证自定义存储无需暴露 GORM 即可刷新统计。
func TestStatsWithoutDatabaseHandle(t *testing.T) {
	a := &aggregateOnlyAction{}
	model := &struct{ CreatedAt time.Time }{}
	rows, err := Exec(context.Background(), a, StatSpec{Code: "opaque", Fact: model, Grain: GrainDay, Metrics: []StatMetric{{Kind: MetricCount}}}, ExecOptions{})
	require.NoError(t, err)
	require.Equal(t, "2", rows[0].Metrics["row_count"])
	require.Equal(t, "CreatedAt", a.query.TimeField)
	store := NewStore()
	spec := StatSpec{Code: "opaque", Fact: model, Grain: GrainDay, Metrics: []StatMetric{{Kind: MetricCount}}}
	snapshot, err := Refresh(context.Background(), store, a, spec, ExecOptions{})
	require.NoError(t, err)
	require.Equal(t, rows, snapshot.Rows)
	stored, ok := store.Get("opaque")
	require.True(t, ok)
	require.Equal(t, "2", stored.Rows[0].Metrics["row_count"])
	ResetReportsForTest()
	t.Cleanup(ResetReportsForTest)
	RegisterReports(ReportDef{Code: "opaque-report", Title: "存储透明性报表", Service: "test", SpecCode: "opaque", Kind: ReportKindLine, MetricAliases: []string{"row_count"}})
	view, err := BuildReportView(store, "test", "opaque-report")
	require.NoError(t, err)
	require.False(t, view.Empty)
	require.Equal(t, "2", view.Summary[0].Value)
}
