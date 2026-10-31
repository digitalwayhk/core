// 本文件在结构初始化边界映射金额类型，不改变业务模型或已有列。
package oltp

import (
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"reflect"
)

// configurePostgresModelTypes 在建表锁内设置当前连接缓存的 GORM 字段类型。
func configurePostgresModelTypes(stmt *gorm.Statement) {
	for _, field := range stmt.Schema.Fields {
		t := field.FieldType
		for t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		if t == reflect.TypeOf(decimal.Decimal{}) && field.TagSettings["TYPE"] == "" {
			field.DataType = schema.DataType("numeric")
		}
	}
}
