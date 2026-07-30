package stats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRegisterAndBuildReportView(t *testing.T) {
	ResetReportsForTest()
	t.Cleanup(ResetReportsForTest)

	RegisterReports(ReportDef{
		Code:        "demo-daily",
		Service:     "shop-order",
		Title:       "日趋势",
		Kind:        ReportKindLine,
		SpecCode:    "order.by_day",
		MetricAlias: "amount_sum",
		Sort:        1,
	})

	menus := ListReportMenus("shop-order")
	require.Len(t, menus, 1)
	require.Equal(t, "/report/shop-order/demo-daily", menus[0].Path)

	store := NewStore()
	store.Put(Snapshot{
		Code:       "order.by_day",
		Grain:      GrainDay,
		ComputedAt: time.Now().UTC(),
		Rows: []StatRow{
			{Bucket: "2026-07-01", Metrics: map[string]string{"amount_sum": "100", "row_count": "2"}},
			{Bucket: "2026-07-02", Metrics: map[string]string{"amount_sum": "50", "row_count": "1"}},
		},
	})

	view, err := BuildReportView(store, "shop-order", "demo-daily")
	require.NoError(t, err)
	require.False(t, view.Empty)
	require.Len(t, view.Series, 2)
	require.NotEmpty(t, view.TableRows)
}

func TestBuildReportViewEmpty(t *testing.T) {
	ResetReportsForTest()
	t.Cleanup(ResetReportsForTest)
	RegisterReports(ReportDef{
		Code:     "x",
		Service:  "s",
		Title:    "t",
		SpecCode: "missing",
		Kind:     ReportKindBar,
	})
	view, err := BuildReportView(NewStore(), "s", "x")
	require.NoError(t, err)
	require.True(t, view.Empty)
}
