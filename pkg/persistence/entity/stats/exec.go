// 本文件仅转换业务统计契约，数据库编译与执行由持久化层负责。
package stats

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"strconv"
	"strings"
	"time"
)

// ExecOptions 保留公开执行参数；Dialect 仅为旧调用兼容提示。
type ExecOptions struct {
	Dialect DialectName
	Range   QueryRange
	Limit   int
}

// Exec 把统计定义提交给模型绑定的持久化能力。
func Exec(ctx context.Context, action types.IDataAction, spec StatSpec, opt ExecOptions) ([]StatRow, error) {
	spec = normalizeSpec(spec)
	if err := Validate(spec); err != nil {
		return nil, err
	}
	q := types.AggregateQuery{Code: spec.Code, Fact: spec.Fact, TimeField: spec.TimeField, Grain: types.TimeGrain(spec.Grain), Range: types.QueryRange{From: opt.Range.From, To: opt.Range.To}, Limit: opt.Limit}
	for _, d := range spec.Dimensions {
		q.Dimensions = append(q.Dimensions, types.AggregateDimension{Field: d.Field, BaseModel: d.BaseModel, DisplayFields: d.DisplayFields, NoDisplay: d.NoDisplay, DisplayFromFact: d.DisplayFromFact, Alias: d.Alias})
	}
	for _, m := range spec.Metrics {
		q.Metrics = append(q.Metrics, types.AggregateMetric{Kind: types.MetricKind(m.Kind), Field: m.Field, Alias: m.Alias})
	}
	rows, err := oltp.AggregateWithDialect(ctx, action, q, string(opt.Dialect))
	if err != nil {
		return nil, err
	}
	result := make([]StatRow, 0, len(rows))
	for _, r := range rows {
		v := StatRow{Grain: TimeGrain(r.Grain), Bucket: r.Bucket, Metrics: r.Metrics, FactDisplay: r.FactDisplay, Dims: map[string]StatDimValue{}}
		for k, d := range r.Dims {
			v.Dims[k] = StatDimValue{ID: d.ID, Displays: d.Displays}
		}
		result = append(result, v)
	}
	return result, nil
}

// scanMaps 仅供既有 ClickHouse 引擎使用 SQL 存储扫描能力。
func scanMaps(db *gorm.DB, sql string, args ...interface{}) ([]map[string]interface{}, error) {
	return oltp.ScanAggregateRows(db, sql, args...)
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
