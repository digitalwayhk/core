package oltp

import (
	"reflect"
	"strings"

	"gorm.io/gorm/schema"
)

// modelTableName 返回 GORM 对模型实际使用的表名；自定义 TableName 优先于默认命名策略。
func modelTableName(namer schema.Namer, modelType reflect.Type) string {
	for modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	if modelType.Kind() == reflect.Struct {
		if tabler, ok := reflect.New(modelType).Interface().(schema.Tabler); ok {
			if tableName := strings.TrimSpace(tabler.TableName()); tableName != "" {
				return tableName
			}
		}
	}
	return namer.TableName(modelType.Name())
}
