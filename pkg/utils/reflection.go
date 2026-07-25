// 本文件提供反射类型识别、字段访问、结构遍历和实例创建能力。
package utils

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"
)

// TypeKind 表示工具包归一后的反射类型类别。
type TypeKind int

const (
	Base    TypeKind = iota // Base 表示数字、布尔、字符串和复数等基础类型。
	Array                   // Array 表示数组、切片、映射和通道等集合类型。
	Ptr                     // Ptr 表示指针类型。
	Func                    // Func 表示函数或接口类型。
	Struct                  // Struct 表示结构体类型。
	Invalid                 // Invalid 表示未归类或 nil 类型。
)

// StopMemoryMonitor 为旧反射缓存监控的兼容入口。
// Deprecated: utils 不再启动包级内存监控，调用该函数不会执行操作。
func StopMemoryMonitor() {}

// ==================== 类型实例创建 ====================

// NewInterface 按对象的元素类型创建一个零值指针实例。
func NewInterface(obj interface{}) interface{} {
	if obj == nil {
		return nil
	}
	tye, _ := GetTypeAndValue(obj)
	return NewInterfaceByType(tye)
}

// NewInterfaceByType 按类型创建一个零值实例（指针）。
// 说明：此前基于 sync.Pool 的对象池因缺少归还路径（RecycleObject 从未被调用）而从未生效，
// 且路由层已有独立对象池，故此处直接用 reflect.New 创建全新零值对象，语义等价且更简洁。
func NewInterfaceByType(typ reflect.Type) interface{} {
	if typ == nil {
		return nil
	}
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

// GetPackageName 返回对象元素类型的完整包路径。
func GetPackageName(target interface{}) string {
	if target == nil {
		return ""
	}
	return getType(reflect.TypeOf(target)).PkgPath()
}

// GetTypeName 返回对象元素类型的名称。
func GetTypeName(item interface{}) string {
	if item == nil {
		return ""
	}
	return getType(reflect.TypeOf(item)).Name()
}

// GetTypeKind 将 reflect.Type 归一为 TypeKind。
func GetTypeKind(typ reflect.Type) TypeKind {
	if typ == nil {
		return Invalid
	}
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

// GetTypeAndValue 返回对象解引用一层后的类型和值。
func GetTypeAndValue(target interface{}) (reflect.Type, reflect.Value) {
	if target == nil {
		return nil, reflect.Value{}
	}
	stype := reflect.TypeOf(target)
	sv := reflect.ValueOf(target)
	if stype.Kind() == reflect.Ptr {
		stype = stype.Elem()
		if sv.IsNil() {
			return stype, reflect.Value{}
		}
		sv = sv.Elem()
	}
	return stype, sv
}

func getType(typ reflect.Type) reflect.Type {
	if typ == nil {
		return nil
	}
	if IsTypeKind(typ, Ptr) {
		return typ.Elem()
	}
	return typ
}

// GetElem 返回指针元素类型，非指针类型原样返回。
func GetElem(typ reflect.Type) reflect.Type {
	return getType(typ)
}

// IsPtr 报告对象的直接类型是否为指针。
func IsPtr(instance interface{}) bool {
	if instance == nil {
		return false
	}
	return reflect.TypeOf(instance).Kind() == reflect.Ptr
}

// IsTypeKind 报告反射类型是否属于指定归一类别。
func IsTypeKind(typ reflect.Type, kind TypeKind) bool {
	return GetTypeKind(typ) == kind
}

// HasProperty 报告结构体是否包含指定 Go 字段。
func HasProperty(target interface{}, name string) bool {
	if target == nil {
		return false
	}
	stype := reflect.TypeOf(target)
	if stype.Kind() == reflect.Ptr {
		stype = stype.Elem()
	}
	if stype.Kind() != reflect.Struct {
		return false
	}
	_, ok := stype.FieldByName(name)
	return ok
}

// GetPropertyType 按 Go 字段名或 JSON tag 查找结构字段。
func GetPropertyType(target interface{}, name string) *reflect.StructField {
	if target == nil {
		return nil
	}
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
	if target == nil {
		return nil
	}
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

// GetPropertyValue 返回结构体指定字段的值，字段缺失时返回空字符串。
func GetPropertyValue(target interface{}, name string) interface{} {
	stype, sv := GetTypeAndValue(target)
	if stype != nil && sv.IsValid() && stype.Kind() == reflect.Struct {
		if _, ok := stype.FieldByName(name); ok {
			return sv.FieldByName(name).Interface()
		}
	}
	return ""
}

// SetPropertyValue 按不区分大小写的字段名转换并设置值。
func SetPropertyValue(sender interface{}, name string, value interface{}) error {
	stype, sv := GetTypeAndValue(sender)
	if stype != nil && sv.IsValid() && stype.Kind() == reflect.Struct {
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

// GetParentType 返回旧继承结构中与当前类型同名的匿名结构字段类型。
func GetParentType(target interface{}) interface{} {
	stype := getType(reflect.TypeOf(target))
	if stype == nil || stype.Kind() != reflect.Struct {
		return nil
	}
	for i := 0; i < stype.NumField(); i++ {
		t := stype.Field(i).Type
		if t.Kind() == reflect.Struct && t.Name() == stype.Name() {
			return reflect.ValueOf(t)
		}
	}
	return nil
}

// ==================== 深度遍历（修复循环引用问题） ====================

// DeepForItem 深度枚举对象的导出字段。
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

// DeepFor 深度枚举指定反射类型的导出字段。
func DeepFor(stype reflect.Type, forfunc func(field, parent reflect.StructField, kind TypeKind)) {
	if stype == nil {
		return
	}
	visited := make(map[reflect.Type]bool)
	deepFor(stype, reflect.StructField{}, forfunc, visited)
}

// 修复：通过 visited 集合防止无限递归，defer 解除标记允许同类型在不同路径中正确遍历
func deepFor(stype reflect.Type, parent reflect.StructField, forfunc func(field, parent reflect.StructField, kind TypeKind), visited map[reflect.Type]bool) {
	stype = getType(stype)
	if stype == nil || stype.Kind() != reflect.Struct {
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

// ForItem 使用回调返回值更新对象的可设置字段。
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

// ForEach 逐个回调对象的导出字段名和值。
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

// ArrayEach 逐个回调数组或切片元素。
func ArrayEach(items interface{}, f func(item interface{})) {
	if items == nil || f == nil {
		return
	}
	stype := reflect.TypeOf(items)
	if stype.Kind() == reflect.Array || stype.Kind() == reflect.Slice {
		s := reflect.ValueOf(items)
		for i := 0; i < s.Len(); i++ {
			f(s.Index(i).Interface())
		}
	}
}

// IsArray 报告对象或其指针元素是否为数组或切片。
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

// NewArrayItem 创建数组或切片元素类型的零值指针实例。
func NewArrayItem(items interface{}) interface{} {
	if items == nil {
		return nil
	}
	stype := reflect.TypeOf(items)
	if stype.Kind() == reflect.Ptr {
		stype = stype.Elem()
	}
	if stype.Kind() == reflect.Array || stype.Kind() == reflect.Slice {
		return NewInterfaceByType(stype.Elem())
	}
	return nil
}
