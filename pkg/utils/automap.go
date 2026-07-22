// 本文件提供结构字段自动映射、自定义映射和字段索引缓存。
package utils

import (
	"reflect"
	"sync"
)

// AutoMapArge 描述自定义字段映射时的源字段和目标对象上下文。
type AutoMapArge struct {
	Field       string
	Value       string
	FieldValue  interface{}
	SourceName  string
	TargetName  string
	SourceType  reflect.Type
	TargetType  reflect.Type
	FieldType   reflect.StructField
	SourceValue interface{}
	TargetValue interface{}
}

// AutoMapHander 描述自定义映射返回的单字段或多字段赋值。
type AutoMapHander struct {
	TargetField string
	TargetValue interface{}
	TargetItems map[string]interface{}
}

// ==================== AutoMap 映射（修复 getmap 被忽略） ====================

var fieldMappingCache sync.Map // key: string, value: []fieldMapping

type fieldMapping struct {
	SourceField string
	TargetField string
	SourceIndex int
	TargetIndex int
}

func getCacheKey(sourceType, targetType reflect.Type) string {
	return sourceType.String() + "->" + targetType.String()
}

func autoMapConvertList(source, target interface{}, getmap func(*AutoMapArge) *AutoMapHander) []interface{} {
	targetType := reflect.TypeOf(target)
	if targetType.Kind() == reflect.Ptr {
		targetType = reflect.ValueOf(target).Elem().Type()
	}
	items := make([]interface{}, 0)
	if st := reflect.TypeOf(source).Kind(); st == reflect.Array || st == reflect.Slice {
		s := reflect.ValueOf(source)
		for i := 0; i < s.Len(); i++ {
			t := NewInterface(targetType)
			AutoMapConvert(s.Index(i).Interface(), t, getmap)
			items = append(items, t)
		}
	}
	return items
}

// AutoMapConvert 将同名字段从源对象映射到目标对象，并允许调用方覆盖映射结果。
func AutoMapConvert(source, target interface{}, getmap func(*AutoMapArge) *AutoMapHander) interface{} {
	stype, sv := GetTypeAndValue(source)
	if stype.Kind() == reflect.Array || stype.Kind() == reflect.Slice {
		return autoMapConvertList(source, target, getmap)
	}

	_, tv := GetTypeAndValue(target)
	targetType := tv.Type()

	cacheKey := getCacheKey(stype, targetType)
	var mappings []fieldMapping
	if cached, ok := fieldMappingCache.Load(cacheKey); ok {
		mappings = cached.([]fieldMapping)
	} else {
		mappings = buildFieldMappings(stype, targetType)
		fieldMappingCache.Store(cacheKey, mappings)
	}

	for _, mapping := range mappings {
		sourceField := sv.Field(mapping.SourceIndex)
		targetField := tv.Field(mapping.TargetIndex)
		if !targetField.CanSet() || !sourceField.IsValid() {
			continue
		}

		// 调用自定义映射函数
		if getmap != nil {
			arge := &AutoMapArge{
				Field:       mapping.SourceField,
				SourceName:  stype.Name(),
				TargetName:  targetType.Name(),
				SourceType:  stype,
				TargetType:  targetType,
				FieldType:   stype.Field(mapping.SourceIndex),
				FieldValue:  sourceField.Interface(),
				SourceValue: source,
				TargetValue: target,
			}
			if handler := getmap(arge); handler != nil {
				if handler.TargetField != "" {
					// 设置指定的目标字段
					if tf := tv.FieldByName(handler.TargetField); tf.CanSet() && handler.TargetValue != nil {
						if v, err := valueToTypeValue(handler.TargetValue, tf.Type()); err == nil {
							tf.Set(v)
						}
					}
				} else if handler.TargetValue != nil {
					if v, err := valueToTypeValue(handler.TargetValue, targetField.Type()); err == nil {
						targetField.Set(v)
					}
				}
				// 批量设置多个字段
				for fieldName, val := range handler.TargetItems {
					if tf := tv.FieldByName(fieldName); tf.CanSet() && val != nil {
						if v, err := valueToTypeValue(val, tf.Type()); err == nil {
							tf.Set(v)
						}
					}
				}
				continue
			}
		}

		// 默认映射
		if sourceField.Type() == targetField.Type() {
			targetField.Set(sourceField)
		} else {
			convertAndSet(sourceField, targetField)
		}
	}

	return target
}

// buildFieldMappings 修复：直接使用 FieldByName 返回的 Index，去掉冗余的 O(n²) 二次循环
func buildFieldMappings(sourceType, targetType reflect.Type) []fieldMapping {
	mappings := make([]fieldMapping, 0, sourceType.NumField())
	for i := 0; i < sourceType.NumField(); i++ {
		sf := sourceType.Field(i)
		if tf, ok := targetType.FieldByName(sf.Name); ok {
			mappings = append(mappings, fieldMapping{
				SourceField: sf.Name,
				TargetField: tf.Name,
				SourceIndex: i,
				TargetIndex: tf.Index[0], // 直接使用，无需二次遍历
			})
		}
	}
	return mappings
}

func convertAndSet(source, target reflect.Value) {
	if source.Type().ConvertibleTo(target.Type()) {
		target.Set(source.Convert(target.Type()))
		return
	}
	str := convertString(source)
	if convertedValue, err := convertOp1(str, target.Type()); err == nil {
		target.Set(convertedValue)
	}
}
