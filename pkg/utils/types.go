package utils

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unsafe"

	"github.com/shopspring/decimal"
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

func GetPackageName(target interface{}) string {
	if target == nil {
		return ""
	}
	return getType(reflect.TypeOf(target)).PkgPath()
}
func GetTypeName(item interface{}) string {
	typ := getType(reflect.TypeOf(item))
	return typ.Name()
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
func NewInterface(obj interface{}) interface{} {
	tye, _ := GetTypeAndValue(obj)
	return reflect.New(tye).Interface()
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
	return reflect.TypeOf(instance).Kind() == reflect.Ptr
}
func IsTypeKind(typ reflect.Type, kind TypeKind) bool {
	return GetTypeKind(typ) == kind
}
func GetPropertyType(target interface{}, name string) *reflect.StructField {
	stype := reflect.TypeOf(target)
	if stype.Kind() == reflect.Ptr { // 如果是指针类型，则获取指针指向的类型
		stype = stype.Elem()
		sf, ok := stype.FieldByName(name)
		if ok {
			return &sf
		}
	}
	return nil
}
func GetPropertyValue(target interface{}, name string) interface{} {
	stype, sv := GetTypeAndValue(target)
	if stype.Kind() == reflect.Struct {
		_, ok := stype.FieldByName(name)
		if ok {
			value := sv.FieldByName(name)
			return value.Interface()
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
			if err == nil {
				vv.Set(v)
			} else {
				return fmt.Errorf("设置%s属性值%s异常:%s", name, value, err)
			}
		}
	}
	return nil
}
func valueToTypeValue(value interface{}, changeType reflect.Type) (reflect.Value, error) {
	vtype := reflect.TypeOf(value)
	vv := reflect.ValueOf(value)
	if vtype != changeType {
		ss := convertString(vv)
		return convertOp1(ss, vtype)
	}
	return vv, nil
}
func GetParentType(target interface{}) interface{} {
	stype := reflect.TypeOf(target)
	count := stype.NumField()
	for i := 0; i < count; i++ {
		t := stype.Field(i).Type
		if t.Kind() == reflect.Struct && t.Name() == stype.Name() {
			return reflect.ValueOf(t)
		}
	}
	return nil
}

func DeepForItem(item interface{}, forfunc func(field, parent reflect.StructField, kind TypeKind)) {
	t := reflect.TypeOf(item)
	DeepFor(t, forfunc)
}
func DeepFor(stype reflect.Type, forfunc func(field, parent reflect.StructField, kind TypeKind)) {
	deepFor(stype, reflect.StructField{}, forfunc)
}
func deepFor(stype reflect.Type, parent reflect.StructField, forfunc func(field, parent reflect.StructField, kind TypeKind)) {
	stype = getType(stype)
	if stype.Kind() == reflect.Struct {
		count := stype.NumField()
		for i := 0; i < count; i++ {
			field := stype.Field(i)
			if unicode.IsLower([]rune(field.Name)[0]) {
				continue
			}
			if forfunc != nil {
				if !field.Anonymous && field.Type.Name() != field.Name {
					ftype := GetTypeKind(field.Type)
					forfunc(field, parent, ftype)
				} else {
					if IsTypeKind(field.Type, Array) {
						deepFor(field.Type.Elem(), field, forfunc)
					} else {
						deepFor(field.Type, field, forfunc)
					}
				}
			}
		}
	}
}
func ForItem(item interface{}, value func(name string) interface{}) {
	sv := reflect.ValueOf(item).Elem()
	DeepForItem(item, func(field, parent reflect.StructField, kind TypeKind) {
		vv := sv.FieldByName(field.Name)
		v := value(field.Name)
		if v != nil {
			vv.Set(reflect.ValueOf(v))
		}
	})
}
func ForEach(item interface{}, fn func(name string, value interface{})) {
	sv := reflect.ValueOf(item).Elem()
	DeepForItem(item, func(field, parent reflect.StructField, kind TypeKind) {
		fn(field.Name, sv.FieldByName(field.Name).Interface())
	})
}
func ArrayEach(items interface{}, f func(item interface{})) {
	stype := reflect.TypeOf(items)
	if stype.Kind() == reflect.Array || stype.Kind() == reflect.Slice {
		s := reflect.ValueOf(items)
		for i := 0; i < s.Len(); i++ {
			oo := s.Index(i).Interface()
			f(oo)
		}
	}
}
func IsArray(items interface{}) bool {
	stype := reflect.TypeOf(items).Elem()
	return stype.Kind() == reflect.Array || stype.Kind() == reflect.Slice
}
func NewArrayItem(items interface{}) interface{} {
	stype := reflect.TypeOf(items)
	if stype.Kind() == reflect.Ptr {
		stype = stype.Elem()
	}
	if stype.Kind() == reflect.Array || stype.Kind() == reflect.Slice {
		t1 := stype.Elem()
		if t1.Kind() == reflect.Ptr {
			t1 = t1.Elem()
		}
		return reflect.New(t1).Interface()
	}
	return nil
}

// AutoMapConvertList 映射转换集合,targetType为转换后的目标类型
func autoMapConvertList(source, target interface{}, getmap func(arge *AutoMapArge) *AutoMapHander) []interface{} {
	targetType := reflect.TypeOf(target)
	if targetType.Kind() == reflect.Ptr {
		targetType = reflect.ValueOf(target).Elem().Type()
	}
	st := reflect.TypeOf(source).Kind()
	items := make([]interface{}, 0)
	if st == reflect.Array || st == reflect.Slice {
		s := reflect.ValueOf(source)
		for i := 0; i < s.Len(); i++ {
			oo := s.Index(i).Interface()
			t := reflect.New(targetType).Interface()
			AutoMapConvert(oo, t, getmap)
			items = append(items, t)
		}
	}
	return items
}

// AutoMapConvert 映射转换
func AutoMapConvert(source, target interface{}, getmap func(arge *AutoMapArge) *AutoMapHander) interface{} {
	stype, sv := GetTypeAndValue(source)
	if stype.Kind() == reflect.Array || stype.Kind() == reflect.Slice {
		return autoMapConvertList(source, target, getmap)
	}
	_, tv := GetTypeAndValue(target)
	sk := stype.Kind()
	if sk == reflect.Struct {
		count := stype.NumField()
		for i := 0; i < count; i++ {
			sfv := sv.Field(i)
			val := convertString(sfv)
			field := stype.Field(i)
			name := field.Name
			var amh *AutoMapHander = nil
			if getmap != nil {
				arge := &AutoMapArge{
					Field:       name,
					FieldType:   field,
					SourceName:  stype.Name(),
					SourceType:  stype,
					TargetName:  tv.Type().Name(),
					TargetType:  tv.Type(),
					Value:       val,
					FieldValue:  sfv.Interface(),
					SourceValue: source,
					TargetValue: target,
				}
				amh = getmap(arge)
				if amh == nil {
					continue
				}
				if amh != nil && amh.TargetField != "" {
					name = amh.TargetField
				}
			}
			vv := tv.FieldByName(name)
			if vv.CanSet() && amh != nil {
				if amh.TargetValue != nil {
					pl := reflect.ValueOf(amh.TargetValue)
					vv.Set(pl)
				} else {
					// vv.Set(convertOp(val, vv.Type()))
					vv.Set(sfv)
				}
				if amh.TargetItems != nil && len(amh.TargetItems) > 0 {
					for n := range amh.TargetItems {
						vv = tv.FieldByName(n)
						if vv.CanSet() {
							pl := reflect.ValueOf(amh.TargetItems[n])
							vv.Set(pl)
						}
					}
				}
			}
		}
	}
	return target
}
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
		{
			i := value.Int()
			return strconv.FormatInt(i, 10)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		{
			i := value.Uint()
			return strconv.FormatUint(i, 10)
		}
	case reflect.Float32, reflect.Float64:
		{
			size := getIntSize(value.Kind())
			return strconv.FormatFloat(value.Float(), 'f', -1, size)
		}
	case reflect.Bool:
		{
			return strconv.FormatBool(value.Bool())
		}
	case reflect.String:
		{
			return value.String()
		}
	}
	return ""
}
func getIntSize(kind reflect.Kind) int {
	size := 64
	if kind == reflect.Int8 || kind == reflect.Uint8 {
		size = 8
	}
	if kind == reflect.Int16 || kind == reflect.Uint16 {
		size = 16
	}
	if kind == reflect.Int32 || kind == reflect.Uint32 || kind == reflect.Float32 {
		size = 32
	}
	return size
}
func convertOp(val string, src reflect.Type) reflect.Value {
	v, _ := convertOp1(val, src)
	return v
}
func convertOp1(val string, src reflect.Type) (reflect.Value, error) {
	ret := reflect.New(src).Elem()
	switch src.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		{
			size := getIntSize(src.Kind())
			v, err := strconv.ParseInt(val, 0, size)
			if err == nil {
				ret.SetInt(v)
			}
			return ret, err
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		{
			size := getIntSize(src.Kind())
			v, err := strconv.ParseUint(val, 0, size)
			if err == nil {
				ret.SetUint(v)
			}
			return ret, err
		}

	case reflect.Float32, reflect.Float64:
		{
			size := getIntSize(src.Kind())
			v, err := strconv.ParseFloat(val, size)
			if err == nil {
				ret.SetFloat(v)
			}
			return ret, err
		}

	case reflect.Bool:
		{
			v, err := strconv.ParseBool(val)
			if err == nil {
				ret.SetBool(v)
			}
			return ret, err
		}

	case reflect.String:
		{
			ret.SetString(val)
			return ret, nil
		}
	}
	return ret, nil
}

func Add(v1, v2 reflect.Value) reflect.Value {
	tye := reflect.TypeOf(v1.Interface())
	num := reflect.New(tye).Elem()
	dec := decimal.NewFromFloat(0)
	decType := reflect.TypeOf(dec)
	if decType == tye {
		d1 := v1.Interface().(decimal.Decimal)
		d2 := v2.Interface().(decimal.Decimal)
		d3 := d1.Add(d2)
		num.Set(reflect.ValueOf(d3))
		return num
	}
	vs1 := convertString(v1)
	vs2 := convertString(v2)
	size := getIntSize(tye.Kind())
	switch tye.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		{

			vi1, _ := strconv.ParseInt(vs1, 0, size)
			vi2, _ := strconv.ParseInt(vs2, 0, size)
			num.SetInt(vi1 + vi2)
			break
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		{
			vi1, _ := strconv.ParseUint(vs1, 0, size)
			vi2, _ := strconv.ParseUint(vs2, 0, size)
			num.SetUint(vi1 + vi2)
			break
		}
	case reflect.Float32, reflect.Float64:
		{
			vi1, _ := strconv.ParseFloat(vs1, size)
			vi2, _ := strconv.ParseFloat(vs2, size)
			num.SetFloat(vi1 + vi2)
			break
		}
	}
	return num
}
func String2Bytes(s string) []byte {
	sh := (*reflect.StringHeader)(unsafe.Pointer(&s))
	bh := reflect.SliceHeader{
		Data: sh.Data,
		Len:  sh.Len,
		Cap:  sh.Len,
	}
	return *(*[]byte)(unsafe.Pointer(&bh))
}

func Bytes2String(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}
