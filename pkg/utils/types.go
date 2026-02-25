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

var (
	reflectObjectPool = sync.Map{} // key: reflect.Type, value: *sync.Pool
	poolMutex         = sync.RWMutex{}
)

func getObjectPool(t reflect.Type) *sync.Pool {
	if pool, ok := reflectObjectPool.Load(t); ok {
		return pool.(*sync.Pool)
	}

	poolMutex.Lock()
	defer poolMutex.Unlock()

	// 双重检查
	if pool, ok := reflectObjectPool.Load(t); ok {
		return pool.(*sync.Pool)
	}

	newPool := &sync.Pool{
		New: func() interface{} {
			return reflect.New(t).Interface()
		},
	}
	reflectObjectPool.Store(t, newPool)
	return newPool
}

// 修复 NewInterface 函数，使用对象池
func NewInterface(obj interface{}) interface{} {
	tye, _ := GetTypeAndValue(obj)

	return NewInterfaceByType(tye)
}
func NewInterfaceByType(typ reflect.Type) interface{} {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	pool := getObjectPool(typ)
	newObj := pool.Get()

	// 重置对象状态
	resetObject(newObj)

	return newObj
}

// 添加对象重置函数
func resetObject(obj interface{}) {
	if obj == nil {
		return
	}

	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() == reflect.Struct {
		// 重置所有字段为零值
		v.Set(reflect.Zero(v.Type()))
	}
}

// 添加对象回收函数
func RecycleObject(obj interface{}) {
	if obj == nil {
		return
	}

	tye := reflect.TypeOf(obj)
	if tye.Kind() == reflect.Ptr {
		tye = tye.Elem()
	}

	if pool, ok := reflectObjectPool.Load(tye); ok {
		resetObject(obj)
		pool.(*sync.Pool).Put(obj)
	}
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
func HasProperty(target interface{}, name string) bool {
	stype := reflect.TypeOf(target)
	if stype.Kind() == reflect.Ptr { // 如果是指针类型，则获取指针指向的类型
		stype = stype.Elem()
	}
	_, ok := stype.FieldByName(name)
	return ok
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

	// 如果类型相同，直接返回
	if vtype == changeType {
		return vv, nil
	}

	// 如果可以直接转换，使用Convert
	if vtype.ConvertibleTo(changeType) {
		return vv.Convert(changeType), nil
	}

	// 否则，通过字符串中转进行转换
	ss := convertString(vv)
	return convertOp1(ss, changeType)
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
	if item == nil {
		return
	}
	t := reflect.TypeOf(item)
	// 如果是指针，获取指向的类型
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
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
						//if field.Type.Elem() != stype {
						deepFor(field.Type.Elem(), field, forfunc)
						//}
					} else {
						//if field.Type.Elem() != stype {
						deepFor(field.Type, field, forfunc)
						//}
					}
				}
			}
		}
	}
}
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
		v := value(field.Name)
		if v != nil {
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
			oo := s.Index(i).Interface()
			f(oo)
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
		t1 := stype.Elem()
		return NewInterfaceByType(t1)
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
			t := NewInterface(targetType)
			AutoMapConvert(oo, t, getmap)
			items = append(items, t)
		}
	}
	return items
}

var (
	fieldMappingCache = sync.Map{} // key: string (sourceType+targetType), value: []fieldMapping
	mappingCacheMutex = sync.RWMutex{}
)

type fieldMapping struct {
	SourceField string
	TargetField string
	SourceIndex int
	TargetIndex int
}

func getCacheKey(sourceType, targetType reflect.Type) string {
	return sourceType.String() + "->" + targetType.String()
}

// 优化 AutoMapConvert，使用缓存减少反射
func AutoMapConvert(source, target interface{}, getmap func(arge *AutoMapArge) *AutoMapHander) interface{} {
	stype, sv := GetTypeAndValue(source)
	if stype.Kind() == reflect.Array || stype.Kind() == reflect.Slice {
		return autoMapConvertList(source, target, getmap)
	}

	_, tv := GetTypeAndValue(target)
	targetType := tv.Type()

	// 使用缓存的字段映射
	cacheKey := getCacheKey(stype, targetType)
	var mappings []fieldMapping

	if cached, ok := fieldMappingCache.Load(cacheKey); ok {
		mappings = cached.([]fieldMapping)
	} else {
		mappings = buildFieldMappings(stype, targetType)
		fieldMappingCache.Store(cacheKey, mappings)
	}

	// 使用缓存的映射进行快速复制
	for _, mapping := range mappings {
		sourceField := sv.Field(mapping.SourceIndex)
		targetField := tv.Field(mapping.TargetIndex)

		if targetField.CanSet() && sourceField.IsValid() {
			// 直接赋值，避免字符串转换
			if sourceField.Type() == targetField.Type() {
				targetField.Set(sourceField)
			} else {
				// 只在类型不匹配时才进行转换
				convertAndSet(sourceField, targetField)
			}
		}
	}

	return target
}

func buildFieldMappings(sourceType, targetType reflect.Type) []fieldMapping {
	mappings := make([]fieldMapping, 0, sourceType.NumField())

	for i := 0; i < sourceType.NumField(); i++ {
		sourceField := sourceType.Field(i)

		if targetField, ok := targetType.FieldByName(sourceField.Name); ok {
			// 找到对应的目标字段索引
			for j := 0; j < targetType.NumField(); j++ {
				if targetType.Field(j).Name == targetField.Name {
					mappings = append(mappings, fieldMapping{
						SourceField: sourceField.Name,
						TargetField: targetField.Name,
						SourceIndex: i,
						TargetIndex: j,
					})
					break
				}
			}
		}
	}

	return mappings
}

func convertAndSet(source, target reflect.Value) {
	// 避免通过字符串转换，直接类型转换
	if source.Type().ConvertibleTo(target.Type()) {
		target.Set(source.Convert(target.Type()))
		return
	}

	// 只在必要时使用字符串转换
	str := convertString(source)
	if convertedValue, err := convertOp1(str, target.Type()); err == nil {
		target.Set(convertedValue)
	}
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

// 添加内存监控
func init() {
	startReflectionMemoryMonitor()
}

func startReflectionMemoryMonitor() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			// 如果内存使用超过阈值，清理缓存
			if m.Alloc > 200*1024*1024 { // 200MB
				cleanReflectionCaches()
			}

			// 记录统计
			logReflectionStats(&m)
		}
	}()
}

func cleanReflectionCaches() {
	// 清理对象池（保留常用类型）
	count := 0
	reflectObjectPool.Range(func(key, value interface{}) bool {
		count++
		if count > 50 { // 只保留前50个常用类型
			reflectObjectPool.Delete(key)
		}
		return true
	})

	// 清理字段映射缓存
	mappingCount := 0
	fieldMappingCache.Range(func(key, value interface{}) bool {
		mappingCount++
		if mappingCount > 100 { // 只保留前100个映射
			fieldMappingCache.Delete(key)
		}
		return true
	})

	// 强制GC
	runtime.GC()

	logx.Infof("清理反射缓存 - 对象池: %d, 映射缓存: %d", count, mappingCount)
}

func logReflectionStats(m *runtime.MemStats) {
	poolCount := 0
	reflectObjectPool.Range(func(key, value interface{}) bool {
		poolCount++
		return true
	})

	mappingCount := 0
	fieldMappingCache.Range(func(key, value interface{}) bool {
		mappingCount++
		return true
	})

	logx.Infof("反射统计 - 内存: %dMB, 对象池: %d, 映射缓存: %d",
		m.Alloc/1024/1024, poolCount, mappingCount)
}
