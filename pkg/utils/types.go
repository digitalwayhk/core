package utils

import (
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unsafe"

	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/logx"
)

type Decimal decimal.Decimal

func (d Decimal) Equals(other Decimal) bool {
	return decimal.Decimal(d).Equal(decimal.Decimal(other))
}

func (d Decimal) Less(other Decimal) bool {
	return decimal.Decimal(d).LessThan(decimal.Decimal(other))
}

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

type AutoMapHander struct {
	TargetField string
	TargetValue interface{}
	TargetItems map[string]interface{}
}

type TypeKind int

const (
	Base TypeKind = iota
	Array
	Ptr
	Func
	Struct
	Invalid
)

// ==================== 内存监控 ====================

var memMonitorStop = make(chan struct{}, 1)

func init() {
	startReflectionMemoryMonitor()
}

// StopMemoryMonitor 优雅停止后台内存监控 goroutine
func StopMemoryMonitor() {
	select {
	case memMonitorStop <- struct{}{}:
	default:
	}
}

func startReflectionMemoryMonitor() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-memMonitorStop:
				return
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				if m.Alloc > 200*1024*1024 { // 仅超过 200MB 时清理并打印
					cleanReflectionCaches(&m)
				}
			}
		}
	}()
}

func cleanReflectionCaches(m *runtime.MemStats) {
	mappingCount, mappingDeleted := 0, 0
	fieldMappingCache.Range(func(key, _ interface{}) bool {
		mappingCount++
		if mappingCount > 100 {
			fieldMappingCache.Delete(key)
			mappingDeleted++
		}
		return true
	})

	runtime.GC()
	logx.Infof("反射缓存清理 - 内存: %dMB, 映射缓存删除: %d",
		m.Alloc/1024/1024, mappingDeleted)
}

// ==================== 类型实例创建 ====================

func NewInterface(obj interface{}) interface{} {
	tye, _ := GetTypeAndValue(obj)
	return NewInterfaceByType(tye)
}

// NewInterfaceByType 按类型创建一个零值实例（指针）。
// 说明：此前基于 sync.Pool 的对象池因缺少归还路径（RecycleObject 从未被调用）而从未生效，
// 且路由层已有独立对象池，故此处直接用 reflect.New 创建全新零值对象，语义等价且更简洁。
func NewInterfaceByType(typ reflect.Type) interface{} {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	return reflect.New(typ).Interface()
}

// RecycleObject 保留旧对象池 API 的源码兼容性。
//
// NewInterfaceByType 现在直接创建全新零值对象，路由层对象复用由独立池负责；
// 因此这里不再执行实际归还动作。
func RecycleObject(obj interface{}) {}

// ==================== 类型工具 ====================

func GetPackageName(target interface{}) string {
	if target == nil {
		return ""
	}
	return getType(reflect.TypeOf(target)).PkgPath()
}

func GetTypeName(item interface{}) string {
	return getType(reflect.TypeOf(item)).Name()
}

func GetTypeKind(typ reflect.Type) TypeKind {
	switch typ.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16,
		reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8,
		reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64,
		reflect.Bool, reflect.String, reflect.Complex64, reflect.Complex128:
		return Base
	case reflect.Array, reflect.Slice, reflect.Map, reflect.Chan:
		return Array
	case reflect.Ptr, reflect.UnsafePointer:
		return Ptr
	case reflect.Func, reflect.Interface:
		return Func
	case reflect.Struct:
		return Struct
	}
	return Invalid
}

func GetTypeAndValue(target interface{}) (reflect.Type, reflect.Value) {
	stype := reflect.TypeOf(target)
	sv := reflect.ValueOf(target)
	if stype.Kind() == reflect.Ptr {
		stype = stype.Elem()
		sv = sv.Elem()
	}
	return stype, sv
}

func getType(typ reflect.Type) reflect.Type {
	if IsTypeKind(typ, Ptr) {
		return typ.Elem()
	}
	return typ
}

func GetElem(typ reflect.Type) reflect.Type {
	return getType(typ)
}

func IsPtr(instance interface{}) bool {
	if instance == nil {
		return false
	}
	return reflect.TypeOf(instance).Kind() == reflect.Ptr
}

func IsTypeKind(typ reflect.Type, kind TypeKind) bool {
	return GetTypeKind(typ) == kind
}

func HasProperty(target interface{}, name string) bool {
	stype := reflect.TypeOf(target)
	if stype.Kind() == reflect.Ptr {
		stype = stype.Elem()
	}
	_, ok := stype.FieldByName(name)
	return ok
}

func GetPropertyType(target interface{}, name string) *reflect.StructField {
	stype := reflect.TypeOf(target)
	if stype.Kind() == reflect.Ptr {
		stype = stype.Elem()
	}
	if stype.Kind() != reflect.Struct {
		return nil
	}
	if sf, ok := stype.FieldByName(name); ok {
		return &sf
	}
	for i := 0; i < stype.NumField(); i++ {
		sf := stype.Field(i)
		tag := sf.Tag.Get("json")
		if idx := strings.Index(tag, ","); idx != -1 {
			tag = tag[:idx]
		}
		if tag == name {
			return &sf
		}
	}
	return nil
}

// GetPropertyTypeByElemName 通过字段元素类型名查找字段
// 例如字段 PriceSources []*IndexPriceSourceModel，elemTypeName="IndexPriceSourceModel" 可以找到该字段
func GetPropertyTypeByElemName(target interface{}, elemTypeName string) *reflect.StructField {
	stype := reflect.TypeOf(target)
	if stype.Kind() == reflect.Ptr {
		stype = stype.Elem()
	}
	if stype.Kind() != reflect.Struct {
		return nil
	}
	for i := 0; i < stype.NumField(); i++ {
		sf := stype.Field(i)
		t := sf.Type
		// 解引用指针
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		// 解引用切片/数组元素
		if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
			t = t.Elem()
			if t.Kind() == reflect.Ptr {
				t = t.Elem()
			}
		}
		if t.Name() == elemTypeName {
			return &sf
		}
	}
	return nil
}

func GetPropertyValue(target interface{}, name string) interface{} {
	stype, sv := GetTypeAndValue(target)
	if stype.Kind() == reflect.Struct {
		if _, ok := stype.FieldByName(name); ok {
			return sv.FieldByName(name).Interface()
		}
	}
	return ""
}

func SetPropertyValue(sender interface{}, name string, value interface{}) error {
	stype, sv := GetTypeAndValue(sender)
	if stype.Kind() == reflect.Struct {
		vv := sv.FieldByNameFunc(func(fieldName string) bool {
			return strings.EqualFold(fieldName, name)
		})
		if vv.CanSet() && value != nil {
			v, err := valueToTypeValue(value, vv.Type())
			if err != nil {
				return fmt.Errorf("设置%s属性值%v异常:%s", name, value, err)
			}
			vv.Set(v)
		}
	}
	return nil
}

func valueToTypeValue(value interface{}, changeType reflect.Type) (reflect.Value, error) {
	vtype := reflect.TypeOf(value)
	vv := reflect.ValueOf(value)
	if vtype == changeType {
		return vv, nil
	}
	if vtype.ConvertibleTo(changeType) {
		return vv.Convert(changeType), nil
	}
	ss := convertString(vv)
	return convertOp1(ss, changeType)
}

func GetParentType(target interface{}) interface{} {
	stype := reflect.TypeOf(target)
	for i := 0; i < stype.NumField(); i++ {
		t := stype.Field(i).Type
		if t.Kind() == reflect.Struct && t.Name() == stype.Name() {
			return reflect.ValueOf(t)
		}
	}
	return nil
}

// ==================== 深度遍历（修复循环引用问题） ====================

func DeepForItem(item interface{}, forfunc func(field, parent reflect.StructField, kind TypeKind)) {
	if item == nil {
		return
	}
	t := reflect.TypeOf(item)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	DeepFor(t, forfunc)
}

func DeepFor(stype reflect.Type, forfunc func(field, parent reflect.StructField, kind TypeKind)) {
	visited := make(map[reflect.Type]bool)
	deepFor(stype, reflect.StructField{}, forfunc, visited)
}

// 修复：通过 visited 集合防止无限递归，defer 解除标记允许同类型在不同路径中正确遍历
func deepFor(stype reflect.Type, parent reflect.StructField, forfunc func(field, parent reflect.StructField, kind TypeKind), visited map[reflect.Type]bool) {
	stype = getType(stype)
	if stype.Kind() != reflect.Struct {
		return
	}
	if visited[stype] {
		return
	}
	visited[stype] = true
	defer func() { visited[stype] = false }()

	for i := 0; i < stype.NumField(); i++ {
		field := stype.Field(i)
		if unicode.IsLower([]rune(field.Name)[0]) {
			continue
		}
		if forfunc == nil {
			continue
		}
		if !field.Anonymous {
			forfunc(field, parent, GetTypeKind(field.Type))
		} else {
			elemType := field.Type
			if IsTypeKind(field.Type, Array) {
				elemType = field.Type.Elem()
			}
			deepFor(elemType, field, forfunc, visited)
		}
	}
}

// ==================== 集合遍历 ====================

func ForItem(item interface{}, value func(name string) interface{}) {
	if item == nil {
		return
	}
	sv := reflect.ValueOf(item)
	if sv.Kind() == reflect.Ptr {
		if sv.IsNil() {
			return
		}
		sv = sv.Elem()
	}
	if !sv.IsValid() {
		return
	}
	DeepForItem(item, func(field, parent reflect.StructField, kind TypeKind) {
		vv := sv.FieldByName(field.Name)
		if !vv.IsValid() || !vv.CanSet() {
			return
		}
		if v := value(field.Name); v != nil {
			vv.Set(reflect.ValueOf(v))
		}
	})
}

func ForEach(item interface{}, fn func(name string, value interface{})) {
	if item == nil {
		return
	}
	sv := reflect.ValueOf(item)
	if sv.Kind() == reflect.Ptr {
		if sv.IsNil() {
			return
		}
		sv = sv.Elem()
	}
	if !sv.IsValid() {
		return
	}
	DeepForItem(item, func(field, parent reflect.StructField, kind TypeKind) {
		fn(field.Name, sv.FieldByName(field.Name).Interface())
	})
}

func ArrayEach(items interface{}, f func(item interface{})) {
	stype := reflect.TypeOf(items)
	if stype.Kind() == reflect.Array || stype.Kind() == reflect.Slice {
		s := reflect.ValueOf(items)
		for i := 0; i < s.Len(); i++ {
			f(s.Index(i).Interface())
		}
	}
}

func IsArray(items interface{}) bool {
	if items == nil {
		return false
	}
	stype := reflect.TypeOf(items)
	if stype.Kind() == reflect.Ptr {
		stype = stype.Elem()
	}
	return stype.Kind() == reflect.Array || stype.Kind() == reflect.Slice
}

func NewArrayItem(items interface{}) interface{} {
	stype := reflect.TypeOf(items)
	if stype.Kind() == reflect.Ptr {
		stype = stype.Elem()
	}
	if stype.Kind() == reflect.Array || stype.Kind() == reflect.Slice {
		return NewInterfaceByType(stype.Elem())
	}
	return nil
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

// ==================== 类型转换 ====================

func AnyToTypeData(value interface{}, src reflect.Type) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	str := convertString(reflect.ValueOf(value))
	rv, err := convertOp1(str, src)
	if err != nil {
		return nil, err
	}
	return rv.Interface(), nil
}

func ConvertToString(value interface{}) string {
	return convertString(reflect.ValueOf(value))
}

func convertString(value reflect.Value) string {
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(value.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(value.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(value.Float(), 'f', -1, getIntSize(value.Kind()))
	case reflect.Bool:
		return strconv.FormatBool(value.Bool())
	case reflect.String:
		return value.String()
	}
	return ""
}

func getIntSize(kind reflect.Kind) int {
	switch kind {
	case reflect.Int8, reflect.Uint8:
		return 8
	case reflect.Int16, reflect.Uint16:
		return 16
	case reflect.Int32, reflect.Uint32, reflect.Float32:
		return 32
	default:
		return 64
	}
}

func convertOp1(val string, src reflect.Type) (reflect.Value, error) {
	ret := reflect.New(src).Elem()
	switch src.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(val, 0, getIntSize(src.Kind()))
		if err == nil {
			ret.SetInt(v)
		}
		return ret, err
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(val, 0, getIntSize(src.Kind()))
		if err == nil {
			ret.SetUint(v)
		}
		return ret, err
	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(val, getIntSize(src.Kind()))
		if err == nil {
			ret.SetFloat(v)
		}
		return ret, err
	case reflect.Bool:
		v, err := strconv.ParseBool(val)
		if err == nil {
			ret.SetBool(v)
		}
		return ret, err
	case reflect.String:
		ret.SetString(val)
		return ret, nil
	}
	return ret, nil
}

// ==================== Add（修复 utils.Decimal 支持） ====================

var (
	decimalType      = reflect.TypeOf(decimal.Decimal{})
	utilsDecimalType = reflect.TypeOf(Decimal{})
)

func Add(v1, v2 reflect.Value) reflect.Value {
	tye := v1.Type()
	num := reflect.New(tye).Elem()

	// 修复：同时处理 decimal.Decimal 和 utils.Decimal 具名类型
	if tye == decimalType {
		d1 := v1.Interface().(decimal.Decimal)
		d2 := v2.Interface().(decimal.Decimal)
		num.Set(reflect.ValueOf(d1.Add(d2)))
		return num
	}
	if tye == utilsDecimalType {
		d1 := decimal.Decimal(v1.Interface().(Decimal))
		d2 := decimal.Decimal(v2.Interface().(Decimal))
		num.Set(reflect.ValueOf(Decimal(d1.Add(d2))))
		return num
	}

	size := getIntSize(tye.Kind())
	vs1 := convertString(v1)
	vs2 := convertString(v2)
	switch tye.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		vi1, err1 := strconv.ParseInt(vs1, 10, size)
		vi2, err2 := strconv.ParseInt(vs2, 10, size)
		if err1 != nil || err2 != nil {
			logx.Errorf("Add 整型解析失败: v1=%q(%v) v2=%q(%v)", vs1, err1, vs2, err2)
		}
		num.SetInt(vi1 + vi2)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		vi1, err1 := strconv.ParseUint(vs1, 10, size)
		vi2, err2 := strconv.ParseUint(vs2, 10, size)
		if err1 != nil || err2 != nil {
			logx.Errorf("Add 无符号整型解析失败: v1=%q(%v) v2=%q(%v)", vs1, err1, vs2, err2)
		}
		num.SetUint(vi1 + vi2)
	case reflect.Float32, reflect.Float64:
		vi1, err1 := strconv.ParseFloat(vs1, size)
		vi2, err2 := strconv.ParseFloat(vs2, size)
		if err1 != nil || err2 != nil {
			logx.Errorf("Add 浮点解析失败: v1=%q(%v) v2=%q(%v)", vs1, err1, vs2, err2)
		}
		num.SetFloat(vi1 + vi2)
	}
	return num
}

// ==================== unsafe 转换（Go 1.20+，替换已弃用 API） ====================

func String2Bytes(s string) []byte {
	if s == "" {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func Bytes2String(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func IsEqual(v1, v2 interface{}) bool {
	if v1 == nil || v2 == nil {
		return v1 == v2
	}
	t1 := reflect.TypeOf(v1)
	t2 := reflect.TypeOf(v2)
	if t1 != t2 {
		return false
	}
	return reflect.DeepEqual(v1, v2)
}
