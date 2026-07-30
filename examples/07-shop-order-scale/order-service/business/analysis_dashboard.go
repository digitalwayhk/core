// 本文件将订单 StatsStore 快照组装为管理端 analysis 标准 Dashboard。
package business

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
	"github.com/shopspring/decimal"
)

const (
	// AnalysisDashboardName 看板标识
	AnalysisDashboardName = "shop-order-analysis"

	// 与前端 layout 绑定的 dataName（动态文案，非交易/访问量固定词）
	statTotalAmount   = "总销售额"
	statOrderCount    = "订单笔数"
	statPaidRate      = "同步完成率"
	statQtyTotal      = "销售数量"
	seriesAmountByDay = "销售额分布"
	seriesCountByDay  = "订单量分布"
	rankByProduct     = "商品销售额排名"
	rankBySupplier    = "供应商销售额排名"
)

// BuildOrderAnalysisDashboard 从 OrderStatsStore 组装 analysis 页模型。
// API 只调用本方法 + Store，不直接查 Order 表。
func BuildOrderAnalysisDashboard() stats.Dashboard {
	store := OrderStatsStore
	byDay, _ := store.Get("order.by_day")
	byDayProduct, _ := store.Get("order.by_day_product")
	byMonthSupplier, _ := store.Get("order.by_month_supplier")

	computed := latestComputed(byDay, byDayProduct, byMonthSupplier)

	totalAmount := sumMetric(byDay.Rows, "amount_sum")
	orderCount := sumMetric(byDay.Rows, "row_count")
	qtyTotal := sumMetric(byDayProduct.Rows, "qty_sum")

	// 同步完成率：有数据时用 100% 占位语义——已汇总订单均已进入权威库；无数据 0
	paidRate := "0"
	if orderCount.IsPositive() {
		paidRate = "100"
	}

	dayAmountSeries := rowsToChartValues(byDay.Rows, "amount_sum", true)
	dayCountSeries := rowsToChartValues(byDay.Rows, "row_count", false)

	dash := stats.Dashboard{
		Name:        AnalysisDashboardName,
		Service:     contract.OrderServiceName,
		Title:       "订单经营分析",
		Description: "基于权威库订单聚合快照；由 StatsRunner 定时刷新",
		ComputedAt:  computed,
		Layout: &stats.DashboardLayout{
			IntroDataNames:      []string{statTotalAmount, statOrderCount, statQtyTotal, statPaidRate},
			SalesTabLabel:       "销售额",
			VisitTabLabel:       "订单量",
			SalesSeriesName:     seriesAmountByDay,
			VisitSeriesName:     seriesCountByDay,
			SalesRankingName:    rankByProduct,
			VisitRankingName:    rankBySupplier,
			CategoryTitle:       "商品金额占比",
			OfflineSectionTitle: "供应商趋势",
		},
		Query: &stats.DashboardQueryMeta{
			Path:            "/api/manage/" + contract.OrderServiceName + "/analysis",
			SupportedGrains: []string{"day", "month", "year"},
			DefaultGrain:    "day",
		},
		Statistics: []stats.StatisticItem{
			{
				Code:        "order.total_amount",
				DataName:    statTotalAmount,
				Description: "统计窗口内订单成交金额合计（TotalAmount）",
				Value:       totalAmount.StringFixed(0),
				ValueFormat: "number",
				ValuePrefix: "¥",
				ValueSuffix: "",
				ChartData:   nil,
				Trends: []stats.TrendItem{
					{Label: "统计来源", Value: "order.by_day", Direction: 0},
					{Label: "订单金额", Value: "¥" + totalAmount.StringFixed(0), Direction: 0},
				},
			},
			{
				Code:        "order.order_count",
				DataName:    statOrderCount,
				Description: "统计窗口内订单行数",
				Value:       orderCount.StringFixed(0),
				ValueFormat: "number",
				ChartData: &stats.ChartData{
					Label:     "订单笔数趋势",
					Value:     orderCount.StringFixed(0),
					ChartType: "area",
					Values:    dayCountSeries,
				},
				Trends: []stats.TrendItem{
					{Label: "日均参考", Value: avgOf(dayCountSeries), Direction: 0},
				},
			},
			{
				Code:        "order.qty_total",
				DataName:    statQtyTotal,
				Description: "统计窗口内商品销售数量合计",
				Value:       qtyTotal.StringFixed(0),
				ValueFormat: "number",
				ChartData: &stats.ChartData{
					Label:     "数量分布",
					Value:     qtyTotal.StringFixed(0),
					ChartType: "bar",
					Values:    dayCountSeries,
				},
				Trends: []stats.TrendItem{
					{Label: "数量合计", Value: qtyTotal.StringFixed(0), Direction: 0},
				},
			},
			{
				Code:        "order.sync_rate",
				DataName:    statPaidRate,
				Description: "已进入权威库并完成统计的订单占比（示意）",
				Value:       paidRate,
				ValueFormat: "number",
				ValueSuffix: "%",
				ChartData: &stats.ChartData{
					Label:     "完成率",
					Value:     paidRate,
					MaxValue:  "100",
					ChartType: "progress",
					Values:    []stats.ChartValue{},
				},
				Trends: []stats.TrendItem{
					{Label: "权威库", Value: "MySQL", Direction: 0},
				},
			},
			// 主图序列（不进 intro）
			{
				Code:        "order.series_amount",
				DataName:    seriesAmountByDay,
				Description: "按日销售额柱状序列",
				Value:       "",
				ChartData: &stats.ChartData{
					Label:     "销售额",
					ChartType: "bar",
					Values:    dayAmountSeries,
				},
			},
			{
				Code:        "order.series_count",
				DataName:    seriesCountByDay,
				Description: "按日订单量柱状序列",
				Value:       "",
				ChartData: &stats.ChartData{
					Label:     "订单量",
					ChartType: "bar",
					Values:    dayCountSeries,
				},
			},
		},
		Rankings: []stats.RankingItem{
			buildProductRanking(byDayProduct),
			buildSupplierRanking(byMonthSupplier),
		},
		Categories: []stats.CategoryItem{
			buildProductCategory(byDayProduct),
		},
		TimeTrends: buildSupplierTimeTrends(byMonthSupplier),
	}
	return dash
}

func latestComputed(snaps ...stats.Snapshot) string {
	var t time.Time
	for _, s := range snaps {
		if s.ComputedAt.After(t) {
			t = s.ComputedAt
		}
	}
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func sumMetric(rows []stats.StatRow, alias string) decimal.Decimal {
	total := decimal.Zero
	for _, r := range rows {
		if v, ok := r.Metrics[alias]; ok {
			d, err := decimal.NewFromString(v)
			if err == nil {
				total = total.Add(d)
			}
		}
	}
	return total
}

func rowsToChartValues(rows []stats.StatRow, metric string, asAmount bool) []stats.ChartValue {
	// 按 bucket 排序
	sorted := append([]stats.StatRow(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Bucket < sorted[j].Bucket })
	out := make([]stats.ChartValue, 0, len(sorted))
	for _, r := range sorted {
		y := r.Metrics[metric]
		if y == "" {
			y = "0"
		}
		// 金额可取整展示
		if asAmount {
			if d, err := decimal.NewFromString(y); err == nil {
				y = d.StringFixed(0)
			}
		}
		out = append(out, stats.ChartValue{
			X:    formatBucketLabel(r.Bucket),
			Y:    y,
			Date: r.Bucket,
		})
	}
	return out
}

func formatBucketLabel(bucket string) string {
	// 2026-07-15 -> 07-15; 2026-07 -> 7月
	if len(bucket) == 10 && bucket[4] == '-' {
		return bucket[5:]
	}
	if len(bucket) == 7 && bucket[4] == '-' {
		m := bucket[5:]
		if m[0] == '0' {
			m = m[1:]
		}
		return m + "月"
	}
	return bucket
}

func avgOf(values []stats.ChartValue) string {
	if len(values) == 0 {
		return "0"
	}
	sum := decimal.Zero
	for _, v := range values {
		d, err := decimal.NewFromString(v.Y)
		if err == nil {
			sum = sum.Add(d)
		}
	}
	return sum.Div(decimal.NewFromInt(int64(len(values)))).StringFixed(0)
}

func buildProductRanking(snap stats.Snapshot) stats.RankingItem {
	type agg struct {
		title string
		total decimal.Decimal
	}
	m := map[uint]*agg{}
	for _, r := range snap.Rows {
		dim := r.Dims["product"]
		a := m[dim.ID]
		if a == nil {
			title := dimDisplay(dim, "productName", "productCode")
			if title == "" {
				title = "商品#" + strconv.FormatUint(uint64(dim.ID), 10)
			}
			a = &agg{title: title, total: decimal.Zero}
			m[dim.ID] = a
		}
		if v, ok := r.Metrics["amount_sum"]; ok {
			if d, err := decimal.NewFromString(v); err == nil {
				a.total = a.total.Add(d)
			}
		}
	}
	list := make([]*agg, 0, len(m))
	for _, a := range m {
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].total.GreaterThan(list[j].total) })
	values := make([]stats.RankingValue, 0, len(list))
	for i, a := range list {
		if i >= 10 {
			break
		}
		values = append(values, stats.RankingValue{
			X:    a.title,
			Y:    a.total.StringFixed(0),
			Rank: i + 1,
		})
	}
	return stats.RankingItem{
		Name:     rankByProduct,
		QueryURL: "/api/manage/" + contract.OrderServiceName + "/bizstats/query?code=order.by_day_product",
		Values:   values,
	}
}

func buildSupplierRanking(snap stats.Snapshot) stats.RankingItem {
	type agg struct {
		title string
		total decimal.Decimal
	}
	m := map[uint]*agg{}
	for _, r := range snap.Rows {
		dim := r.Dims["supplier"]
		a := m[dim.ID]
		if a == nil {
			title := dimDisplay(dim, "supplierName", "supplierCode")
			if title == "" {
				title = "供应商#" + strconv.FormatUint(uint64(dim.ID), 10)
			}
			a = &agg{title: title, total: decimal.Zero}
			m[dim.ID] = a
		}
		if v, ok := r.Metrics["amount_sum"]; ok {
			if d, err := decimal.NewFromString(v); err == nil {
				a.total = a.total.Add(d)
			}
		}
	}
	list := make([]*agg, 0, len(m))
	for _, a := range m {
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].total.GreaterThan(list[j].total) })
	values := make([]stats.RankingValue, 0, len(list))
	for i, a := range list {
		if i >= 10 {
			break
		}
		values = append(values, stats.RankingValue{
			X:    a.title,
			Y:    a.total.StringFixed(0),
			Rank: i + 1,
		})
	}
	return stats.RankingItem{
		Name:     rankBySupplier,
		QueryURL: "/api/manage/" + contract.OrderServiceName + "/bizstats/query?code=order.by_month_supplier",
		Values:   values,
	}
}

func buildProductCategory(snap stats.Snapshot) stats.CategoryItem {
	rank := buildProductRanking(snap)
	children := make([]stats.CategoryItem, 0, len(rank.Values))
	total := decimal.Zero
	for _, v := range rank.Values {
		d, _ := decimal.NewFromString(v.Y)
		total = total.Add(d)
		children = append(children, stats.CategoryItem{
			Category: v.X,
			Value:    v.Y,
			Total:    "",
		})
	}
	for i := range children {
		children[i].Total = total.StringFixed(0)
	}
	return stats.CategoryItem{
		Category: "商品",
		Value:    total.StringFixed(0),
		Total:    total.StringFixed(0),
		Children: children,
	}
}

func buildSupplierTimeTrends(snap stats.Snapshot) []stats.TimeTrendItem {
	out := make([]stats.TimeTrendItem, 0, len(snap.Rows))
	for _, r := range snap.Rows {
		dim := r.Dims["supplier"]
		name := dimDisplay(dim, "supplierName", "supplierCode")
		if name == "" {
			name = "供应商#" + strconv.FormatUint(uint64(dim.ID), 10)
		}
		y := r.Metrics["amount_sum"]
		if y == "" {
			y = "0"
		}
		out = append(out, stats.TimeTrendItem{
			X:    formatBucketLabel(r.Bucket),
			Y:    y,
			Name: name,
			Date: r.Bucket,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date == out[j].Date {
			return out[i].Name < out[j].Name
		}
		return out[i].Date < out[j].Date
	})
	return out
}

func dimDisplay(dim stats.StatDimValue, keys ...string) string {
	if dim.Displays == nil {
		return ""
	}
	for _, k := range keys {
		if v := strings.TrimSpace(dim.Displays[k]); v != "" {
			return v
		}
		// 兼容大小写
		for dk, dv := range dim.Displays {
			if strings.EqualFold(dk, k) && strings.TrimSpace(dv) != "" {
				return strings.TrimSpace(dv)
			}
		}
	}
	return ""
}
