// 本文件保留既有方言名称入口；数据库选择与编译已下沉。
package stats

import "github.com/digitalwayhk/core/pkg/persistence/database/oltp"

// DialectName 是旧统计调用的方言提示类型。
type DialectName = oltp.DialectName

const (
	DialectSQLite = oltp.DialectSQLite
	DialectMySQL  = oltp.DialectMySQL
)

// DetectDialect 保留兼容；未知存储返回空值，不再伪装 MySQL。
func DetectDialect(name string) DialectName { return oltp.DetectDialect(name) }
