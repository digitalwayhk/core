// Package stats 提供声明式业务统计契约：Spec 定义事实表、时间粒度、维度与指标，
// 由框架编译为 OLTP 聚合查询；业务不写方言 SQL。
package stats

import "time"

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

// StatDimension 业务事实表上的维度（通常为引用基础资料的 ID）。
//
// DisplayFields：从 BaseModel 解析的展示字段名（如 "Name"、"Code"）。
// 空且 BaseModel 非空且 !NoDisplay 时，默认使用 ["Name"]。
//
// DisplayFromFact：从事实表自身带出的展示列（写入 GROUP BY / SELECT），
// 用于订单快照字段 ProductCode/ProductName 等场景。
type StatDimension struct {
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

// StatMetric 聚合指标。
type StatMetric struct {
	// Kind count | sum | avg
	Kind MetricKind
	// Field sum/avg 必填；count 可空。
	Field string
	// Alias 结果 metrics 键；空则自动生成。
	Alias string
}

// StatSpec 一份业务统计定义（一个稳定 method）。
type StatSpec struct {
	// Code 全局唯一，如 order.by_day_product；任务/API/Store 键。
	Code string
	// Fact 主业务事实 model 指针实例（如 &Order{}）。
	Fact any
	// TimeField 时间字段 Go 名，默认 CreatedAt。
	TimeField string
	// Grain 单一时间粒度（一 Spec 一 grain，键更清晰）。
	Grain TimeGrain
	// Dimensions 业务维度，可多个。
	Dimensions []StatDimension
	// Metrics 指标，可多个；至少一项。
	Metrics []StatMetric
	// Title 人类可读标题。
	Title string
	// Description 用途说明。
	Description string
}

// ResolvedDisplayFields 返回维度最终使用的基础 model 展示字段列表。
func (d StatDimension) ResolvedDisplayFields() []string {
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

// StatDimValue 结果中的维度值。
type StatDimValue struct {
	ID       uint              `json:"id"`
	Displays map[string]string `json:"displays,omitempty"`
}

// StatRow 一行聚合结果。
type StatRow struct {
	Grain       TimeGrain               `json:"grain"`
	Bucket      string                  `json:"bucket"`
	Dims        map[string]StatDimValue `json:"dims,omitempty"`
	Metrics     map[string]string       `json:"metrics"` // decimal 字符串，避免精度丢失
	FactDisplay map[string]string       `json:"factDisplay,omitempty"`
}

// Snapshot 一次刷新写入 Store 的快照。
type Snapshot struct {
	Code       string    `json:"code"`
	Title      string    `json:"title,omitempty"`
	Grain      TimeGrain `json:"grain"`
	ComputedAt time.Time `json:"computedAt"`
	Rows       []StatRow `json:"rows"`
	Error      string    `json:"error,omitempty"`
}
