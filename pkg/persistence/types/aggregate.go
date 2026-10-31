// 本文件定义不含 SQL/GORM/驱动选择的聚合请求和结果契约。
package types

import (
	"context"
	"time"
)

// TimeGrain 时间粒度。
type TimeGrain string

const (
	GrainYear    TimeGrain = "year"
	GrainQuarter TimeGrain = "quarter"
	GrainMonth   TimeGrain = "month"
	GrainWeek    TimeGrain = "week" // ISO 周：YYYY-Www
	GrainDay     TimeGrain = "day"
)

// MetricKind 指标类型，仅 count / sum / avg。
type MetricKind string

const (
	MetricCount MetricKind = "count"
	MetricSum   MetricKind = "sum"
	MetricAvg   MetricKind = "avg"
)

// AggregateDimension 业务事实表上的维度（通常为引用基础资料的 ID）。
//
// DisplayFields：从 BaseModel 解析的展示字段名（如 "Name"、"Code"）。
// 空且 BaseModel 非空且 !NoDisplay 时，默认使用 ["Name"]。
//
// DisplayFromFact：从事实表自身带出的展示列（写入 GROUP BY / SELECT），
// 用于订单快照字段 ProductCode/ProductName 等场景。
type AggregateDimension struct {
	// Field 事实表维度字段（Go 导出字段名，如 ProductID）。
	Field string
	// BaseModel 可选基础资料类型（指针实例）；用于解析 DisplayFields。
	BaseModel any
	// DisplayFields 基础 model 展示字段；空则默认 Name。
	DisplayFields []string
	// NoDisplay 为 true 时不解析任何展示字段，结果仅含 id。
	NoDisplay bool
	// DisplayFromFact 事实表上随维度聚合的展示列（Go 字段名）。
	DisplayFromFact []string
	// Alias 结果 dims 下的键，默认由 Field 推导（去掉 ID 后缀并小写）。
	Alias string
}

// AggregateMetric 聚合指标。
type AggregateMetric struct {
	// Kind count | sum | avg
	Kind MetricKind
	// Field sum/avg 必填；count 可空。
	Field string
	// Alias 结果 metrics 键；空则自动生成。
	Alias string
}

// AggregateQuery 一份业务统计定义（一个稳定 method）。
type AggregateQuery struct {
	// Code 全局唯一，如 order.by_day_product；任务/API/Store 键。
	Code  string
	Range QueryRange
	Limit int
	// Fact 主业务事实 model 指针实例（如 &Order{}）。
	Fact any
	// TimeField 时间字段 Go 名，默认 CreatedAt。
	TimeField string
	// Grain 单一时间粒度（一 Spec 一 grain，键更清晰）。
	Grain TimeGrain
	// Dimensions 业务维度，可多个。
	Dimensions []AggregateDimension
	// Metrics 指标，可多个；至少一项。
	Metrics []AggregateMetric
	// Title 人类可读标题。
	Title string
	// Description 用途说明。
	Description string
}

// ResolvedDisplayFields 返回维度最终使用的基础 model 展示字段列表。
func (d AggregateDimension) ResolvedDisplayFields() []string {
	if d.NoDisplay || d.BaseModel == nil {
		return nil
	}
	if len(d.DisplayFields) == 0 {
		return []string{"Name"}
	}
	out := make([]string, 0, len(d.DisplayFields))
	for _, f := range d.DisplayFields {
		if f != "" {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return []string{"Name"}
	}
	return out
}

// QueryRange 统计时间窗口（UTC 推荐）。
type QueryRange struct {
	From time.Time
	To   time.Time // 不含终点；零值表示不限制上界
}

// AggregateDimensionValue 结果中的维度值。
type AggregateDimensionValue struct {
	ID       uint              `json:"id"`
	Displays map[string]string `json:"displays,omitempty"`
}

// AggregateRow 一行聚合结果。
type AggregateRow struct {
	Grain       TimeGrain                          `json:"grain"`
	Bucket      string                             `json:"bucket"`
	Dims        map[string]AggregateDimensionValue `json:"dims,omitempty"`
	Metrics     map[string]string                  `json:"metrics"` // decimal 字符串，避免精度丢失
	FactDisplay map[string]string                  `json:"factDisplay,omitempty"`
}

// IAggregateAction 是可选的模型聚合能力；现有 IDataAction 无需增加方法。
type IAggregateAction interface {
	Aggregate(context.Context, AggregateQuery) ([]AggregateRow, error)
}
