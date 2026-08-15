package business

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
	"github.com/stretchr/testify/require"
)

func TestBuildOrderAnalysisDashboardEmptyStore(t *testing.T) {
	// 清空 store
	OrderStatsStore = stats.NewStore()
	dash := BuildOrderAnalysisDashboard()
	require.Equal(t, AnalysisDashboardName, dash.Name)
	require.Equal(t, "shop-order", dash.Service)
	require.NotNil(t, dash.Layout)
	require.Len(t, dash.Layout.IntroDataNames, 4)
	require.NotEmpty(t, dash.Statistics)
	// 空数据时金额为 0
	var total *stats.StatisticItem
	for i := range dash.Statistics {
		if dash.Statistics[i].DataName == statTotalAmount {
			total = &dash.Statistics[i]
			break
		}
	}
	require.NotNil(t, total)
	require.Equal(t, "0", total.Value)
	require.Equal(t, "¥", total.ValuePrefix)
}

func TestBuildOrderAnalysisDashboardWithSnapshots(t *testing.T) {
	OrderStatsStore = stats.NewStore()
	OrderStatsStore.Put(stats.Snapshot{
		Code:       "order.by_day",
		Grain:      stats.GrainDay,
		ComputedAt: time.Now().UTC(),
		Rows: []stats.StatRow{
			{
				Grain:   stats.GrainDay,
				Bucket:  "2026-07-01",
				Metrics: map[string]string{"row_count": "2", "amount_sum": "150"},
			},
			{
				Grain:   stats.GrainDay,
				Bucket:  "2026-07-02",
				Metrics: map[string]string{"row_count": "1", "amount_sum": "50"},
			},
		},
	})
	OrderStatsStore.Put(stats.Snapshot{
		Code:       "order.by_day_product",
		Grain:      stats.GrainDay,
		ComputedAt: time.Now().UTC(),
		Rows: []stats.StatRow{
			{
				Bucket: "2026-07-01",
				Dims: map[string]stats.StatDimValue{
					"product": {
						ID: 1,
						Displays: map[string]string{
							"productName": "商品A",
							"productCode": "A1",
						},
					},
				},
				Metrics: map[string]string{"amount_sum": "150", "qty_sum": "3", "row_count": "2"},
			},
		},
	})

	dash := BuildOrderAnalysisDashboard()
	var total *stats.StatisticItem
	for i := range dash.Statistics {
		if dash.Statistics[i].DataName == statTotalAmount {
			total = &dash.Statistics[i]
			break
		}
	}
	require.NotNil(t, total)
	require.Equal(t, "200", total.Value)

	require.NotEmpty(t, dash.Rankings)
	require.Equal(t, rankByProduct, dash.Rankings[0].Name)
	require.NotEmpty(t, dash.Rankings[0].Values)
	require.Equal(t, "商品A", dash.Rankings[0].Values[0].X)
}
