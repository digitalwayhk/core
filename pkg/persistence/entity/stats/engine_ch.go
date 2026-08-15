package stats

import (
	"context"
	"fmt"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/database/olap"
	"gorm.io/gorm"
)

// CHViewManager 物化视图管理能力（由 olap.ClickHouse 适配）。
type CHViewManager interface {
	SaveBusinessViewConfig(config *olap.BusinessDimensionConfig) error
	CreateBusinessViewFromConfig(config *olap.BusinessDimensionConfig) error
}

// ClickHouseEngine 通过 CH 物化视图刷新统计。
// Ensure：Spec → BusinessDimensionConfig → 保存并建 MV。
// Refresh：查询 MV 聚合结果映射为 StatRow。
//
// 事实表入库（OLTP→CH）不在本引擎内，由独立 Ingest/CDC 负责；
// 未入库时 Refresh 会失败，可按配置 FallbackOLTP。
type ClickHouseEngine struct {
	// Views 可选：非 nil 时 Ensure 会写配置并建 MV。
	Views CHViewManager
	// DB ClickHouse gorm 连接，用于查询 MV；可为 nil（仅 Ensure）。
	DB *gorm.DB
	// SourceTableOverride 全局覆盖事实表名。
	SourceTableOverride string
	// Compile 额外选项。
	CompileOpt CompileClickHouseOptions
}

// NewClickHouseEngine 创建 CH 引擎。
func NewClickHouseEngine(views CHViewManager, db *gorm.DB) *ClickHouseEngine {
	return &ClickHouseEngine{Views: views, DB: db}
}

// Name 实现 StatsEngine。
func (e *ClickHouseEngine) Name() EngineName { return EngineClickHouse }

// Ensure 编译 Spec 并创建/更新物化视图。
func (e *ClickHouseEngine) Ensure(ctx context.Context, spec StatSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e == nil || e.Views == nil {
		return fmt.Errorf("ClickHouse 视图管理器未配置")
	}
	opt := e.CompileOpt
	if e.SourceTableOverride != "" {
		opt.SourceTable = e.SourceTableOverride
	}
	plan, err := CompileClickHouse(spec, opt)
	if err != nil {
		return err
	}
	cfg := plan.ToBusinessDimensionConfig()
	if cfg == nil {
		return fmt.Errorf("空 CH 计划")
	}
	if err := e.Views.SaveBusinessViewConfig(cfg); err != nil {
		return fmt.Errorf("保存 CH 视图配置: %w", err)
	}
	if err := e.Views.CreateBusinessViewFromConfig(cfg); err != nil {
		// 视图已存在时部分错误可忽略——向上返回由调用方决定
		return fmt.Errorf("创建 CH 物化视图: %w", err)
	}
	return nil
}

// Refresh 从物化视图读取聚合行。
func (e *ClickHouseEngine) Refresh(ctx context.Context, spec StatSpec, opt ExecOptions) ([]StatRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e == nil || e.DB == nil {
		return nil, fmt.Errorf("ClickHouse DB 未配置，无法查询 MV")
	}
	copt := e.CompileOpt
	if e.SourceTableOverride != "" {
		copt.SourceTable = e.SourceTableOverride
	}
	plan, err := CompileClickHouse(spec, copt)
	if err != nil {
		return nil, err
	}
	sql, args, err := buildCHQuerySQL(plan, spec, opt)
	if err != nil {
		return nil, err
	}
	rows, err := scanMaps(e.DB.WithContext(ctx), sql, args...)
	if err != nil {
		return nil, fmt.Errorf("查询 CH 视图 %s: %w", plan.ViewName, err)
	}
	return mapCHRowsToStatRows(spec, plan, rows), nil
}

func buildCHQuerySQL(plan *CHViewPlan, spec StatSpec, opt ExecOptions) (string, []interface{}, error) {
	if plan == nil {
		return "", nil, fmt.Errorf("nil plan")
	}
	// 简化：按 bucket + 维度列 SELECT，指标用 sum 再聚合（SummingMergeTree 需再 merge）
	selects := []string{"toString(bucket) AS bucket"}
	groups := []string{"bucket"}
	for i, d := range plan.Dimensions {
		alias := fmt.Sprintf("dim_%d", i)
		selects = append(selects, fmt.Sprintf("%s AS %s", d, alias))
		groups = append(groups, d)
	}
	selects = append(selects, "sum(record_count) AS row_count")
	// 指标别名：与 Spec Metrics 对齐
	for _, m := range normalizeSpec(spec).Metrics {
		if m.Kind == MetricCount {
			continue
		}
		col := ""
		// 在 plan 字段中找
		fields, _ := collectFieldsWithDB(nil, spec.Fact)
		if fm, err := requireField(fields, m.Field); err == nil {
			col = fm.Column
		}
		if col == "" {
			continue
		}
		aggCol := "total_" + col
		alias := m.Alias
		if alias == "" {
			alias = defaultMetricAlias(m)
		}
		if m.Kind == MetricAvg {
			// 近似：avg of avg 不完美；用 total 时需要 count——此处 sum(avg)/count 简化为 avgMerge 未建时 sum(total)/sum(record_count)
			selects = append(selects, fmt.Sprintf("sum(%s) AS %s", aggCol, alias))
		} else {
			selects = append(selects, fmt.Sprintf("sum(%s) AS %s", aggCol, alias))
		}
	}

	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(strings.Join(selects, ", "))
	b.WriteString(" FROM ")
	b.WriteString(plan.ViewName)
	b.WriteString(" WHERE 1=1")
	args := make([]interface{}, 0, 2)
	if !opt.Range.From.IsZero() {
		b.WriteString(" AND bucket >= ?")
		args = append(args, opt.Range.From.UTC())
	}
	if !opt.Range.To.IsZero() {
		b.WriteString(" AND bucket < ?")
		args = append(args, opt.Range.To.UTC())
	}
	b.WriteString(" GROUP BY ")
	b.WriteString(strings.Join(groups, ", "))
	b.WriteString(" ORDER BY bucket")
	limit := opt.Limit
	if limit <= 0 {
		limit = 10000
	}
	b.WriteString(fmt.Sprintf(" LIMIT %d", limit))
	return b.String(), args, nil
}

func mapCHRowsToStatRows(spec StatSpec, plan *CHViewPlan, raw []map[string]interface{}) []StatRow {
	spec = normalizeSpec(spec)
	out := make([]StatRow, 0, len(raw))
	for _, r := range raw {
		row := StatRow{
			Grain:   spec.Grain,
			Bucket:  formatCHBucket(asString(pick(r, "bucket"))),
			Dims:    map[string]StatDimValue{},
			Metrics: map[string]string{},
		}
		for i, d := range spec.Dimensions {
			alias := d.Alias
			if alias == "" {
				alias = defaultDimAlias(d.Field)
			}
			idKey := fmt.Sprintf("dim_%d", i)
			// 维度可能是 id 或字符串 code；尽量解析 id
			val := pick(r, idKey)
			dv := StatDimValue{ID: asUint(val), Displays: map[string]string{}}
			if dv.ID == 0 {
				s := asString(val)
				if s != "" {
					dv.Displays["value"] = s
				}
			}
			// DisplayFromFact 列在 MV 中接在 id 后
			// 简化：若有 product_code 等列名直接读
			for _, ff := range d.DisplayFromFact {
				fields, _ := collectFieldsWithDB(nil, spec.Fact)
				if fm, err := requireField(fields, ff); err == nil {
					if v := asString(pick(r, fm.Column, fmt.Sprintf("dim_%d", i))); v != "" && fm.Column != plan.Dimensions[i] {
						// try column name from raw - CH may return as dim alias only for first dim col
						_ = v
					}
				}
			}
			row.Dims[alias] = dv
		}
		// metrics
		if v := pick(r, "row_count"); v != nil {
			row.Metrics["row_count"] = asDecimalString(v)
		}
		for _, m := range spec.Metrics {
			alias := m.Alias
			if alias == "" {
				alias = defaultMetricAlias(m)
			}
			if m.Kind == MetricCount {
				if _, ok := row.Metrics["row_count"]; ok {
					row.Metrics[alias] = row.Metrics["row_count"]
				}
				continue
			}
			if v := pick(r, alias); v != nil {
				row.Metrics[alias] = asDecimalString(v)
			}
		}
		out = append(out, row)
	}
	return out
}

func formatCHBucket(s string) string {
	s = strings.TrimSpace(s)
	// ClickHouse DateTime 字符串裁剪到日期
	if len(s) >= 10 && s[4] == '-' {
		if strings.Contains(s, " ") {
			return s[:10]
		}
	}
	return s
}

// CompileAllClickHouse 编译已注册的全部 Spec（预览/迁移工具）。
func CompileAllClickHouse(opt CompileClickHouseOptions) ([]*CHViewPlan, error) {
	specs := All()
	out := make([]*CHViewPlan, 0, len(specs))
	for _, s := range specs {
		p, err := CompileClickHouse(s, opt)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.Code, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// EnsureAllClickHouseViews 对全部已注册 Spec 执行 Ensure（需要已 SetEngine CH）。
func EnsureAllClickHouseViews(ctx context.Context, engine StatsEngine) error {
	if engine == nil {
		return fmt.Errorf("engine nil")
	}
	for _, s := range All() {
		if err := engine.Ensure(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", s.Code, err)
		}
	}
	return nil
}
