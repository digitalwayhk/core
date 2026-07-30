package stats

import (
	"fmt"
	"strings"
)

// DialectName 后端方言。
type DialectName string

const (
	DialectSQLite DialectName = "sqlite"
	DialectMySQL  DialectName = "mysql"
)

// DetectDialect 根据 dialector 名称猜测。
func DetectDialect(name string) DialectName {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "sqlite"):
		return DialectSQLite
	case strings.Contains(n, "mysql"), strings.Contains(n, "mariadb"):
		return DialectMySQL
	default:
		// 默认按 MySQL 兼容（07 远程权威库）
		return DialectMySQL
	}
}

func quoteIdent(d DialectName, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	// 仅允许简单标识符，防止注入
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		// 回退：去掉非法字符
		return "`" + sanitizeIdent(name) + "`"
	}
	if d == DialectMySQL || d == DialectSQLite {
		return "`" + name + "`"
	}
	return name
}

func sanitizeIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// timeBucketExpr 返回 SELECT/GROUP 中的时间桶表达式，别名固定为 bucket。
func timeBucketExpr(d DialectName, timeCol string, grain TimeGrain) (string, error) {
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
			// %W 周一为一周开始；与 ISO 略有差异，文档约定
			return fmt.Sprintf("strftime('%%Y-W%%W', %s)", col), nil
		case GrainQuarter:
			return fmt.Sprintf("printf('%%s-Q%%d', strftime('%%Y', %s), (CAST(strftime('%%m', %s) AS INTEGER)+2)/3)", col, col), nil
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
