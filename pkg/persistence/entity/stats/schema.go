// 本文件为既有统计校验及 ClickHouse 编译保留模型元数据门面。
package stats

import (
	"fmt"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"gorm.io/gorm"
)

type fieldMeta = oltp.AggregateField

func collectFields(model any) (map[string]fieldMeta, error) { return oltp.AggregateFields(nil, model) }
func collectFieldsWithDB(db *gorm.DB, model any) (map[string]fieldMeta, error) {
	return oltp.AggregateFields(db, model)
}
func resolveTableName(db *gorm.DB, model any) (string, error) {
	return oltp.ResolveAggregateTable(db, model)
}
func requireField(fields map[string]fieldMeta, name string) (fieldMeta, error) {
	m, ok := fields[name]
	if !ok {
		return fieldMeta{}, fmt.Errorf("字段 %s 不存在", name)
	}
	return m, nil
}
