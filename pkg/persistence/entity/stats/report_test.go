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
		Code:          "demo-daily",
		Service:       "shop-order",
		Title:         "订单日趋势",
		Kind:          ReportKindLine,
		SpecCode:      "order.by_day",
		MetricAliases: []string{"row_count", "amount_sum"},
		MetricTitles: map[string]string{
			"row_count":  "订单笔数",
			"amount_sum": "金额",
		},
		ChartTitle: "订单日趋势",
		TableTitle: "订单明细",
		Sort:       1,
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
	// 2 天 × 2 指标 = 4 个序列点
	require.Len(t, view.Series, 4)
	require.Equal(t, "订单笔数", view.Series[0].Name)
	require.Equal(t, "金额", view.Series[2].Name)
	require.Len(t, view.Summary, 2)
	require.Equal(t, "订单笔数", view.Summary[0].DataName)
	require.Equal(t, "3", view.Summary[0].Value)
	require.Equal(t, "金额", view.Summary[1].DataName)
	require.Equal(t, "150", view.Summary[1].Value)
	require.Equal(t, []string{"日期", "订单笔数", "金额"}, view.TableHeaders)
	require.NotEmpty(t, view.TableRows)
	require.Equal(t, "订单日趋势", view.Def.ChartTitleOf())
	require.Equal(t, "订单明细", view.Def.TableTitleOf())
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
