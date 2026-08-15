package stats

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/digitalwayhk/core/pkg/persistence/database/olap"
)

// CHViewPlan 是 StatSpec 编译到 ClickHouse 的中间产物（与方言 SQL 解耦）。
// 可转为 olap.BusinessDimensionConfig 并交给现有 CreateBusinessViewFromConfig。
type CHViewPlan struct {
	// ViewName 物化视图名（由 Spec.Code 规范化）。
	ViewName string `json:"viewName"`
	// SourceTableName CH 侧事实表名（默认由 Fact 类型推断，可覆盖）。
	SourceTableName string `json:"sourceTableName"`
	// TimeGranularity 与 olap 配置一致：day/month/year/week/quarter
	TimeGranularity string `json:"timeGranularity"`
	// TimeField CH 源表时间列（默认 created_at）。
	TimeField string `json:"timeField"`
	// Dimensions 业务维度列名（snake_case）。
	Dimensions []string `json:"dimensions"`
	// NumericFields 整型/浮点聚合字段。
	NumericFields []string `json:"numericFields"`
	// DecimalFields decimal 聚合字段。
	DecimalFields []string `json:"decimalFields"`
	// Description 视图说明。
	Description string `json:"description"`
	// SpecCode 溯源。
	SpecCode string `json:"specCode"`
	// MaterializedViewDDL 可执行的 CREATE MATERIALIZED VIEW（SummingMergeTree + POPULATE）。
	MaterializedViewDDL string `json:"materializedViewDDL,omitempty"`
}

// CompileClickHouseOptions 编译选项。
type CompileClickHouseOptions struct {
	// SourceTable 覆盖 CH 事实表名；空则从 Fact 推断（SingularTable）。
	SourceTable string
	// TimeColumn 覆盖时间列；空则由 Spec.TimeField 映射列名。
	TimeColumn string
	// TTLDays 写入 DDL 的 TTL；0 表示不设。
	TTLDays int
}

// CompileClickHouse 将 StatSpec 编译为 CH 视图计划（不访问网络）。
// 业务仍只维护 Spec；接入 CH 时用本函数 + Ensure 建 MV。
func CompileClickHouse(spec StatSpec, opt CompileClickHouseOptions) (*CHViewPlan, error) {
	spec = normalizeSpec(spec)
	if err := Validate(spec); err != nil {
		return nil, err
	}

	source := strings.TrimSpace(opt.SourceTable)
	if source == "" {
		var err error
		source, err = resolveTableName(nil, spec.Fact)
		if err != nil || source == "" {
			return nil, fmt.Errorf("无法解析 CH 源表名: %v", err)
		}
	}

	timeCol := strings.TrimSpace(opt.TimeColumn)
	if timeCol == "" {
		fields, err := collectFieldsWithDB(nil, spec.Fact)
		if err != nil {
			return nil, err
		}
		tf, err := requireField(fields, spec.TimeField)
		if err != nil {
			return nil, err
		}
		timeCol = tf.Column
	}

	dims := make([]string, 0, len(spec.Dimensions))
	fields, _ := collectFieldsWithDB(nil, spec.Fact)
	for _, d := range spec.Dimensions {
		fm, err := requireField(fields, d.Field)
		if err != nil {
			return nil, err
		}
		dims = append(dims, fm.Column)
		// DisplayFromFact 列也作为维度列进入 MV（与 GROUP BY 一致）
		for _, ff := range d.DisplayFromFact {
			ffm, err := requireField(fields, ff)
			if err != nil {
				return nil, err
			}
			dims = append(dims, ffm.Column)
		}
	}

	var numeric, decimals []string
	for _, m := range spec.Metrics {
		if m.Kind == MetricCount || m.Field == "" {
			continue
		}
		fm, err := requireField(fields, m.Field)
		if err != nil {
			return nil, err
		}
		if isDecimalType(fm) {
			decimals = append(decimals, fm.Column)
		} else {
			numeric = append(numeric, fm.Column)
		}
	}
	// 去重
	numeric = uniqueStrings(numeric)
	decimals = uniqueStrings(decimals)
	dims = uniqueStrings(dims)

	grain := grainToCH(spec.Grain)
	viewName := viewNameFromCode(spec.Code)

	plan := &CHViewPlan{
		ViewName:        viewName,
		SourceTableName: source,
		TimeGranularity: grain,
		TimeField:       timeCol,
		Dimensions:      dims,
		NumericFields:   numeric,
		DecimalFields:   decimals,
		Description:     firstNonEmpty(spec.Description, spec.Title, "stats:"+spec.Code),
		SpecCode:        spec.Code,
	}
	plan.MaterializedViewDDL = buildCHMaterializedViewDDL(plan, opt.TTLDays)
	return plan, nil
}

// ToBusinessDimensionConfig 转为现有 olap 配置，便于 Save/CreateBusinessViewFromConfig。
func (p *CHViewPlan) ToBusinessDimensionConfig() *olap.BusinessDimensionConfig {
	if p == nil {
		return nil
	}
	dimsJSON, _ := json.Marshal(p.Dimensions)
	numJSON, _ := json.Marshal(p.NumericFields)
	decJSON, _ := json.Marshal(p.DecimalFields)
	return &olap.BusinessDimensionConfig{
		ViewName:        p.ViewName,
		SourceTableName: p.SourceTableName,
		Dimensions:      string(dimsJSON),
		TimeGranularity: p.TimeGranularity,
		NumericFields:   string(numJSON),
		DecimalFields:   string(decJSON),
		TimeField:       p.TimeField,
		TTLDays:         0,
		Description:     p.Description,
	}
}

func grainToCH(g TimeGrain) string {
	switch g {
	case GrainDay:
		return "day"
	case GrainMonth:
		return "month"
	case GrainYear:
		return "year"
	case GrainWeek:
		return "week"
	case GrainQuarter:
		return "quarter"
	default:
		return "day"
	}
}

func viewNameFromCode(code string) string {
	// order.by_day_product → stats_order_by_day_product
	var b strings.Builder
	b.WriteString("stats_")
	for _, r := range strings.ToLower(code) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	name := b.String()
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	return strings.Trim(name, "_")
}

func isDecimalType(fm fieldMeta) bool {
	if fm.Type == nil {
		return false
	}
	return fm.Type.String() == "decimal.Decimal" || strings.Contains(fm.Type.String(), "decimal.Decimal")
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// buildCHMaterializedViewDDL 生成简化版 MV DDL（与 olap.CreateBusinessViewFromConfig 语义对齐的精简版）。
// 生产环境优先走 olap.CreateBusinessViewFromConfig；本 DDL 用于预览与无 CH 客户端场景。
func buildCHMaterializedViewDDL(plan *CHViewPlan, ttlDays int) string {
	if plan == nil {
		return ""
	}
	timeFunc := chTimeBucketExpr(plan.TimeField, plan.TimeGranularity)
	selects := []string{fmt.Sprintf("%s AS bucket", timeFunc)}
	groups := []string{timeFunc}
	for _, d := range plan.Dimensions {
		selects = append(selects, d)
		groups = append(groups, d)
	}
	selects = append(selects, "count() AS record_count")
	for _, f := range plan.DecimalFields {
		selects = append(selects,
			fmt.Sprintf("sum(%s) AS total_%s", f, f),
			fmt.Sprintf("avg(%s) AS avg_%s", f, f),
		)
	}
	for _, f := range plan.NumericFields {
		selects = append(selects,
			fmt.Sprintf("sum(%s) AS total_%s", f, f),
			fmt.Sprintf("avg(%s) AS avg_%s", f, f),
		)
	}
	partition := "toYYYYMM(bucket)"
	if plan.TimeGranularity == "year" {
		partition = "toYear(bucket)"
	}
	orderBy := append([]string{"bucket"}, plan.Dimensions...)
	ttl := ""
	if ttlDays > 0 {
		ttl = fmt.Sprintf("\nTTL bucket + INTERVAL %d DAY", ttlDays)
	}
	return fmt.Sprintf(`CREATE MATERIALIZED VIEW IF NOT EXISTS %s
ENGINE = SummingMergeTree()
PARTITION BY %s
ORDER BY (%s)%s
POPULATE
AS SELECT
    %s
FROM %s
GROUP BY %s`,
		plan.ViewName,
		partition,
		strings.Join(orderBy, ", "),
		ttl,
		strings.Join(selects, ",\n    "),
		plan.SourceTableName,
		strings.Join(groups, ", "),
	)
}

func chTimeBucketExpr(timeCol, grain string) string {
	col := timeCol
	if col == "" {
		col = "created_at"
	}
	switch grain {
	case "year":
		return fmt.Sprintf("toStartOfYear(%s)", col)
	case "quarter":
		return fmt.Sprintf("toStartOfQuarter(%s)", col)
	case "month":
		return fmt.Sprintf("toStartOfMonth(%s)", col)
	case "week":
		return fmt.Sprintf("toStartOfWeek(%s)", col)
	default:
		return fmt.Sprintf("toStartOfDay(%s)", col)
	}
}
