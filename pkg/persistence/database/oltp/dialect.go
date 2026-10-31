// 本文件将数据库方言限制在持久化层。
package oltp

import (
	"fmt"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"strings"
)

// DialectName 后端方言。
type DialectName string

const (
	DialectSQLite   DialectName = "sqlite"
	DialectMySQL    DialectName = "mysql"
	DialectPostgres DialectName = "postgres"
)

// DetectDialect 根据 dialector 名称猜测。
func DetectDialect(name string) DialectName {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "sqlite"):
		return DialectSQLite
	case strings.Contains(n, "mysql"), strings.Contains(n, "mariadb"):
		return DialectMySQL
	case n == "postgres":
		return DialectPostgres
	default:
		return ""
	}
}

func quoteIdent(d DialectName, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	if d == DialectPostgres {
		return "\"" + strings.ReplaceAll(name, "\"", "\"\"") + "\""
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// timeBucketExpr 返回 SELECT/GROUP 中的时间桶表达式，别名固定为 bucket。
func timeBucketExpr(d DialectName, timeCol string, grain types.TimeGrain) (string, error) {
	col := quoteIdent(d, timeCol)
	switch d {
	case DialectSQLite:
		switch grain {
		case GrainDay:
			return fmt.Sprintf("strftime('%%Y-%%m-%%d', %s)", col), nil
		case GrainMonth:
			return fmt.Sprintf("strftime('%%Y-%%m', %s)", col), nil
		case GrainYear:
			return fmt.Sprintf("strftime('%%Y', %s)", col), nil
		case GrainWeek:
			// ISO 周年跨年边界与其他 OLTP 保持一致。
			return fmt.Sprintf("strftime('%%G-W%%V', %s)", col), nil
		case GrainQuarter:
			return fmt.Sprintf("printf('%%s-Q%%d', strftime('%%Y', %s), (CAST(strftime('%%m', %s) AS INTEGER)+2)/3)", col, col), nil
		}
	case DialectPostgres:
		format := map[types.TimeGrain]string{GrainDay: "YYYY-MM-DD", GrainMonth: "YYYY-MM", GrainYear: "YYYY", GrainWeek: "IYYY-\"W\"IW", GrainQuarter: "YYYY-\"Q\"Q"}[grain]
		if format != "" {
			return fmt.Sprintf("to_char(%s AT TIME ZONE 'UTC', '%s')", col, format), nil
		}

	case DialectMySQL:
		switch grain {
		case GrainDay:
			return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m-%%d')", col), nil
		case GrainMonth:
			return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m')", col), nil
		case GrainYear:
			return fmt.Sprintf("DATE_FORMAT(%s, '%%Y')", col), nil
		case GrainWeek:
			return fmt.Sprintf("DATE_FORMAT(%s, '%%x-W%%v')", col), nil // ISO 周
		case GrainQuarter:
			return fmt.Sprintf("CONCAT(YEAR(%s), '-Q', QUARTER(%s))", col, col), nil
		}
	}
	return "", fmt.Errorf("方言 %s 不支持 grain %s", d, grain)
}

const (
	GrainDay     = types.GrainDay
	GrainMonth   = types.GrainMonth
	GrainYear    = types.GrainYear
	GrainQuarter = types.GrainQuarter
	GrainWeek    = types.GrainWeek
	MetricCount  = types.MetricCount
	MetricSum    = types.MetricSum
	MetricAvg    = types.MetricAvg
)
