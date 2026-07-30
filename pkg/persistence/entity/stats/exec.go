package stats

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// ExecOptions 控制一次统计执行。
type ExecOptions struct {
	// Dialect 空则尝试从 action 的 gorm 连接探测。
	Dialect DialectName
	// Range 时间窗口。
	Range QueryRange
	// Limit 最大返回行数，0 默认 10000。
	Limit int
}

// Exec 按 Spec 在 OLTP 上执行聚合，返回行结果。
// 业务任务层调用；API 层不得调用。
// 使用参数化 gorm.Raw，不经由 IDataAction.Raw（该接口不支持绑定参数）。
func Exec(ctx context.Context, action types.IDataAction, spec StatSpec, opt ExecOptions) ([]StatRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if action == nil {
		return nil, fmt.Errorf("IDataAction 不能为空")
	}
	spec = normalizeSpec(spec)
	if err := Validate(spec); err != nil {
		return nil, err
	}
	gormDB, err := openGorm(action, spec.Fact)
	if err != nil {
		return nil, err
	}
	dialect := opt.Dialect
	if dialect == "" {
		if gormDB.Dialector != nil {
			dialect = DetectDialect(gormDB.Dialector.Name())
		} else {
			dialect = DialectMySQL
		}
	}
	sql, args, colPlan, err := compileOLTP(gormDB, spec, dialect, opt)
	if err != nil {
		return nil, err
	}
	// 使用 []map 时部分驱动会把列值扫成指针；改用 Rows 手动读更稳。
	rawRows, err := scanMaps(gormDB.WithContext(ctx), sql, args...)
	if err != nil {
		return nil, fmt.Errorf("stats exec: %w", err)
	}
	rows := mapRawRows(spec, colPlan, rawRows)
	if err := enrichBaseDisplays(ctx, gormDB, dialect, spec, rows); err != nil {
		// 展示失败不丢指标
		_ = err
	}
	return rows, nil
}

type columnPlan struct {
	BucketAlias string
	DimCols     []dimCol
	FactDisp    []factDispCol
	MetricCols  []metricCol
}

type dimCol struct {
	Alias  string
	IDCol  string // SQL 结果别名
	Field  string
	Idx    int
	FromID bool
}

type factDispCol struct {
	DimAlias string
	GoField  string
	SQLAlias string
	Key      string // displays 键：小写字段名
}

type metricCol struct {
	Alias    string
	SQLAlias string
	Kind     MetricKind
}

func compileOLTP(db *gorm.DB, spec StatSpec, dialect DialectName, opt ExecOptions) (string, []interface{}, columnPlan, error) {
	// 表名/列名一律从当前 gorm 连接 Schema 解析（SingularTable、TableName() 等）
	factFields, err := collectFieldsWithDB(db, spec.Fact)
	if err != nil {
		return "", nil, columnPlan{}, err
	}
	table, err := resolveTableName(db, spec.Fact)
	if err != nil || table == "" {
		return "", nil, columnPlan{}, fmt.Errorf("无法解析 fact 表名: %v", err)
	}
	timeMeta, _ := requireField(factFields, spec.TimeField)
	bucketExpr, err := timeBucketExpr(dialect, timeMeta.Column, spec.Grain)
	if err != nil {
		return "", nil, columnPlan{}, err
	}

	plan := columnPlan{BucketAlias: "bucket"}
	selects := []string{fmt.Sprintf("%s AS %s", bucketExpr, quoteIdent(dialect, "bucket"))}
	groups := []string{bucketExpr}

	for i, d := range spec.Dimensions {
		fm, _ := requireField(factFields, d.Field)
		idAlias := fmt.Sprintf("dim_%d_id", i)
		selects = append(selects, fmt.Sprintf("%s AS %s", quoteIdent(dialect, fm.Column), quoteIdent(dialect, idAlias)))
		groups = append(groups, quoteIdent(dialect, fm.Column))
		plan.DimCols = append(plan.DimCols, dimCol{
			Alias:  d.Alias,
			IDCol:  idAlias,
			Field:  d.Field,
			Idx:    i,
			FromID: true,
		})
		for j, ff := range d.DisplayFromFact {
			ffm, _ := requireField(factFields, ff)
			sqlAlias := fmt.Sprintf("dim_%d_fd_%d", i, j)
			// 聚合下取 MAX 保证确定性
			selects = append(selects, fmt.Sprintf("MAX(%s) AS %s", quoteIdent(dialect, ffm.Column), quoteIdent(dialect, sqlAlias)))
			plan.FactDisp = append(plan.FactDisp, factDispCol{
				DimAlias: d.Alias,
				GoField:  ff,
				SQLAlias: sqlAlias,
				Key:      lowerFirst(ff),
			})
		}
	}

	for i, m := range spec.Metrics {
		sqlAlias := fmt.Sprintf("m_%d", i)
		var expr string
		switch m.Kind {
		case MetricCount:
			expr = "COUNT(1)"
		case MetricSum:
			fm, _ := requireField(factFields, m.Field)
			expr = fmt.Sprintf("SUM(%s)", quoteIdent(dialect, fm.Column))
		case MetricAvg:
			fm, _ := requireField(factFields, m.Field)
			expr = fmt.Sprintf("AVG(%s)", quoteIdent(dialect, fm.Column))
		}
		selects = append(selects, fmt.Sprintf("%s AS %s", expr, quoteIdent(dialect, sqlAlias)))
		plan.MetricCols = append(plan.MetricCols, metricCol{Alias: m.Alias, SQLAlias: sqlAlias, Kind: m.Kind})
	}

	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(strings.Join(selects, ", "))
	b.WriteString(" FROM ")
	b.WriteString(quoteIdent(dialect, table))
	b.WriteString(" WHERE 1=1")

	args := make([]interface{}, 0, 2)
	timeCol := quoteIdent(dialect, timeMeta.Column)
	if !opt.Range.From.IsZero() {
		b.WriteString(" AND ")
		b.WriteString(timeCol)
		b.WriteString(" >= ?")
		args = append(args, opt.Range.From.UTC())
	}
	if !opt.Range.To.IsZero() {
		b.WriteString(" AND ")
		b.WriteString(timeCol)
		b.WriteString(" < ?")
		args = append(args, opt.Range.To.UTC())
	}
	b.WriteString(" GROUP BY ")
	b.WriteString(strings.Join(groups, ", "))
	b.WriteString(" ORDER BY ")
	b.WriteString(quoteIdent(dialect, "bucket"))

	limit := opt.Limit
	if limit <= 0 {
		limit = 10000
	}
	b.WriteString(" LIMIT ")
	b.WriteString(strconv.Itoa(limit))

	return b.String(), args, plan, nil
}

func mapRawRows(spec StatSpec, plan columnPlan, raw []map[string]interface{}) []StatRow {
	out := make([]StatRow, 0, len(raw))
	for _, r := range raw {
		row := StatRow{
			Grain:   spec.Grain,
			Bucket:  asString(pick(r, plan.BucketAlias, "bucket")),
			Dims:    map[string]StatDimValue{},
			Metrics: map[string]string{},
		}
		for _, dc := range plan.DimCols {
			id := asUint(pick(r, dc.IDCol))
			row.Dims[dc.Alias] = StatDimValue{ID: id, Displays: map[string]string{}}
		}
		for _, fd := range plan.FactDisp {
			dv := row.Dims[fd.DimAlias]
			if dv.Displays == nil {
				dv.Displays = map[string]string{}
			}
			dv.Displays[fd.Key] = asString(pick(r, fd.SQLAlias))
			row.Dims[fd.DimAlias] = dv
		}
		for _, mc := range plan.MetricCols {
			row.Metrics[mc.Alias] = asDecimalString(pick(r, mc.SQLAlias))
		}
		out = append(out, row)
	}
	return out
}

// enrichBaseDisplays 按 BaseModel + DisplayFields 批量补全展示（默认 Name）。
func enrichBaseDisplays(ctx context.Context, gormDB *gorm.DB, dialect DialectName, spec StatSpec, rows []StatRow) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if gormDB == nil {
		return nil
	}
	for _, d := range spec.Dimensions {
		displayFields := d.ResolvedDisplayFields()
		if d.BaseModel == nil || len(displayFields) == 0 {
			continue
		}
		ids := make(map[uint]struct{})
		for _, row := range rows {
			if dv, ok := row.Dims[d.Alias]; ok && dv.ID > 0 {
				ids[dv.ID] = struct{}{}
			}
		}
		if len(ids) == 0 {
			continue
		}
		idList := make([]uint, 0, len(ids))
		for id := range ids {
			idList = append(idList, id)
		}
		baseFields, err := collectFieldsWithDB(gormDB, d.BaseModel)
		if err != nil {
			return err
		}
		table, err := resolveTableName(gormDB, d.BaseModel)
		if err != nil || table == "" {
			return fmt.Errorf("无法解析维度表名: %v", err)
		}
		// 主键列：优先 schema 中 PrimaryKey 字段
		idCol := "id"
		if pk, ok := baseFields["ID"]; ok && pk.Column != "" {
			idCol = pk.Column
		}
		cols := []string{quoteIdent(dialect, idCol) + " AS id"}
		for _, df := range displayFields {
			fm, ok := baseFields[df]
			if !ok {
				continue
			}
			cols = append(cols, fmt.Sprintf("%s AS %s", quoteIdent(dialect, fm.Column), quoteIdent(dialect, lowerFirst(df))))
		}
		placeholders := make([]string, len(idList))
		args := make([]interface{}, len(idList))
		for i, id := range idList {
			placeholders[i] = "?"
			args[i] = id
		}
		sql := fmt.Sprintf("SELECT %s FROM %s WHERE %s IN (%s)",
			strings.Join(cols, ", "),
			quoteIdent(dialect, table),
			quoteIdent(dialect, idCol),
			strings.Join(placeholders, ","),
		)
		var baseRows []map[string]interface{}
		if err := gormDB.WithContext(ctx).Raw(sql, args...).Scan(&baseRows).Error; err != nil {
			return err
		}
		byID := map[uint]map[string]string{}
		for _, br := range baseRows {
			id := asUint(pick(br, "id"))
			disp := map[string]string{}
			for _, df := range displayFields {
				key := lowerFirst(df)
				disp[key] = asString(pick(br, key))
			}
			byID[id] = disp
		}
		for i := range rows {
			dv := rows[i].Dims[d.Alias]
			if disp, ok := byID[dv.ID]; ok {
				if dv.Displays == nil {
					dv.Displays = map[string]string{}
				}
				for k, v := range disp {
					if v != "" {
						dv.Displays[k] = v
					}
				}
				rows[i].Dims[d.Alias] = dv
			}
		}
	}
	return nil
}

func openGorm(action types.IDataAction, model any) (*gorm.DB, error) {
	if action == nil {
		return nil, fmt.Errorf("IDataAction 不能为空")
	}
	// 优先按 fact 初始化连接（解析 GetRemoteDBName 等）
	if model != nil {
		if db, err := action.GetModelDB(model); err == nil {
			if g, ok := db.(*gorm.DB); ok && g != nil {
				return g, nil
			}
		}
	}
	if run := action.GetRunDB(); run != nil {
		if g, ok := run.(*gorm.DB); ok && g != nil {
			return g, nil
		}
	}
	if db, err := action.GetModelDB(nil); err == nil {
		if g, ok := db.(*gorm.DB); ok && g != nil {
			return g, nil
		}
	}
	return nil, fmt.Errorf("无法从 IDataAction 获取 *gorm.DB")
}

func scanMaps(db *gorm.DB, sql string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, err := db.Raw(sql, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		raw := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]interface{}, len(cols))
		for i, c := range cols {
			m[c] = derefScanValue(raw[i])
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func derefScanValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []byte:
		return string(t)
	case *[]byte:
		if t == nil {
			return nil
		}
		return string(*t)
	case *string:
		if t == nil {
			return nil
		}
		return *t
	case *int64:
		if t == nil {
			return nil
		}
		return *t
	case *float64:
		if t == nil {
			return nil
		}
		return *t
	default:
		return v
	}
}

func pick(m map[string]interface{}, keys ...string) interface{} {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
		// 驱动可能返回大小写不同
		for mk, mv := range m {
			if strings.EqualFold(mk, k) {
				return mv
			}
		}
	}
	return nil
}

func asString(v interface{}) string {
	if v == nil {
		return ""
	}
	v = derefScanValue(v)
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case time.Time:
		return t.Format("2006-01-02")
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func asUint(v interface{}) uint {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case uint:
		return t
	case uint64:
		return uint(t)
	case int64:
		if t < 0 {
			return 0
		}
		return uint(t)
	case int:
		if t < 0 {
			return 0
		}
		return uint(t)
	case float64:
		if t < 0 {
			return 0
		}
		return uint(t)
	case []byte:
		n, _ := strconv.ParseUint(string(t), 10, 64)
		return uint(n)
	case string:
		n, _ := strconv.ParseUint(t, 10, 64)
		return uint(n)
	default:
		n, _ := strconv.ParseUint(fmt.Sprint(t), 10, 64)
		return uint(n)
	}
}

func asDecimalString(v interface{}) string {
	if v == nil {
		return "0"
	}
	switch t := v.(type) {
	case decimal.Decimal:
		return t.String()
	case float64:
		return decimal.NewFromFloat(t).String()
	case float32:
		return decimal.NewFromFloat32(t).String()
	case int64:
		return decimal.NewFromInt(t).String()
	case int:
		return decimal.NewFromInt(int64(t)).String()
	case []byte:
		d, err := decimal.NewFromString(string(t))
		if err != nil {
			return string(t)
		}
		return d.String()
	case string:
		d, err := decimal.NewFromString(t)
		if err != nil {
			return t
		}
		return d.String()
	default:
		d, err := decimal.NewFromString(fmt.Sprint(t))
		if err != nil {
			return fmt.Sprint(t)
		}
		return d.String()
	}
}
