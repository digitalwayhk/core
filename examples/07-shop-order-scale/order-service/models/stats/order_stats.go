// Package stats 定义 07 订单服务的声明式业务统计 Spec。
// 仅声明 Fact / 时间粒度 / 维度 / 指标；不写方言 SQL。
package stats

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
)

// OrderByDay
// 用途：经营看板 — 订单按天汇总
// 事实：Order；时间：CreatedAt @ day
// 维度：无
// 指标：row_count、amount_sum(TotalAmount)
var OrderByDay = stats.StatSpec{
	Code:        "order.by_day",
	Fact:        &transaction.Order{},
	TimeField:   "CreatedAt",
	Grain:       stats.GrainDay,
	Title:       "订单按天汇总",
	Description: "按创建日统计订单行数与成交金额合计",
	Metrics: []stats.StatMetric{
		{Kind: stats.MetricCount, Alias: "row_count"},
		{Kind: stats.MetricSum, Field: "TotalAmount", Alias: "amount_sum"},
	},
}

// OrderByDayProduct
// 用途：经营看板 — 按天 × 商品
// 事实：Order；时间：CreatedAt @ day
// 维度：ProductID；展示用事实表快照 ProductCode/ProductName（订单侧已冗余）
// 指标：row_count、amount_sum、qty_sum
var OrderByDayProduct = stats.StatSpec{
	Code:        "order.by_day_product",
	Fact:        &transaction.Order{},
	TimeField:   "CreatedAt",
	Grain:       stats.GrainDay,
	Title:       "订单按天×商品",
	Description: "按创建日与商品汇总订单行数、数量与金额",
	Dimensions: []stats.StatDimension{{
		Field: "ProductID",
		Alias: "product",
		// 无跨库 BaseModel：07 商品主数据在 supplier 服务；展示取订单快照列
		DisplayFromFact: []string{"ProductCode", "ProductName"},
		NoDisplay:       true, // 不走 BaseModel 默认 Name
	}},
	Metrics: []stats.StatMetric{
		{Kind: stats.MetricCount, Alias: "row_count"},
		{Kind: stats.MetricSum, Field: "Quantity", Alias: "qty_sum"},
		{Kind: stats.MetricSum, Field: "TotalAmount", Alias: "amount_sum"},
	},
}

// OrderByMonthSupplier
// 用途：按月 × 供应商
// 维度：SupplierID + 快照 SupplierCode/SupplierName
var OrderByMonthSupplier = stats.StatSpec{
	Code:        "order.by_month_supplier",
	Fact:        &transaction.Order{},
	TimeField:   "CreatedAt",
	Grain:       stats.GrainMonth,
	Title:       "订单按月×供应商",
	Description: "按月与供应商汇总订单行数与金额",
	Dimensions: []stats.StatDimension{{
		Field:           "SupplierID",
		Alias:           "supplier",
		DisplayFromFact: []string{"SupplierCode", "SupplierName"},
		NoDisplay:       true,
	}},
	Metrics: []stats.StatMetric{
		{Kind: stats.MetricCount, Alias: "row_count"},
		{Kind: stats.MetricSum, Field: "TotalAmount", Alias: "amount_sum"},
	},
}

func init() {
	stats.Register(OrderByDay, OrderByDayProduct, OrderByMonthSupplier)
}
