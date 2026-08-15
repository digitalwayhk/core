// 订单服务报表定义：与 Dashboard 分离，挂在服务子菜单 /report/shop-order/{code}。
package stats

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
)

func init() {
	stats.RegisterReports(
		// 1. 订单日趋势：笔数 + 金额 双折线
		stats.ReportDef{
			Code:          "order-daily",
			Service:       contract.OrderServiceName,
			Title:         "订单日趋势",
			MenuTitle:     "日趋势报表",
			Description:   "按日汇总订单笔数与金额",
			Kind:          stats.ReportKindLine,
			SpecCode:      "order.by_day",
			MetricAliases: []string{"row_count", "amount_sum"},
			MetricTitles: map[string]string{
				"row_count":  "订单笔数",
				"amount_sum": "金额",
			},
			ChartTitle:      "订单日趋势",
			TableTitle:      "订单明细",
			Sort:            10,
			DrillReportCode: "order-by-product",
			LinkManagePath:  "/main/shop-order/ordermanage",
			LinkLabel:       "打开订单列表",
		},
		// 2. 商品销售
		stats.ReportDef{
			Code:          "order-by-product",
			Service:       contract.OrderServiceName,
			Title:         "商品销售报表",
			MenuTitle:     "商品销售",
			Description:   "按商品汇总销售额与销量",
			Kind:          stats.ReportKindMixed,
			SpecCode:      "order.by_day_product",
			MetricAliases: []string{"amount_sum", "qty_sum"},
			MetricTitles: map[string]string{
				"amount_sum": "销售额",
				"qty_sum":    "销量",
			},
			MetricAlias:    "amount_sum",
			DimAlias:       "product",
			ChartTitle:     "商品销售",
			TableTitle:     "订单明细",
			Sort:           20,
			LinkManagePath: "/main/shop-order/ordermanage",
			LinkLabel:      "打开订单列表",
		},
		// 3. 供应商月报
		stats.ReportDef{
			Code:        "order-by-supplier",
			Service:     contract.OrderServiceName,
			Title:       "供应商月报",
			MenuTitle:   "供应商月报",
			Description: "按月×供应商汇总金额",
			Kind:        stats.ReportKindBar,
			SpecCode:    "order.by_month_supplier",
			MetricAlias: "amount_sum",
			MetricTitles: map[string]string{
				"amount_sum": "金额",
			},
			DimAlias:       "supplier",
			ChartTitle:     "供应商月报",
			TableTitle:     "订单明细",
			Sort:           30,
			LinkManagePath: "/main/shop-order/ordermanage",
			LinkLabel:      "打开订单列表",
		},
		// 4. 商品占比
		stats.ReportDef{
			Code:        "order-product-share",
			Service:     contract.OrderServiceName,
			Title:       "商品金额占比",
			MenuTitle:   "商品占比",
			Description: "商品销售额占比饼图",
			Kind:        stats.ReportKindPie,
			SpecCode:    "order.by_day_product",
			MetricAlias: "amount_sum",
			MetricTitles: map[string]string{
				"amount_sum": "金额",
			},
			DimAlias:   "product",
			ChartTitle: "商品金额占比",
			TableTitle: "订单明细",
			Sort:       40,
		},
	)
}
