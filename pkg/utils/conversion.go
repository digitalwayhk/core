// 本文件提供基础类型转换、数值相加和安全的字节字符串转换。
package utils

import (
	"reflect"
	"strconv"

	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/logx"
)

// ==================== 类型转换 ====================

// AnyToTypeData 将值转换为指定基础类型。
func AnyToTypeData(value interface{}, src reflect.Type) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	str := convertString(reflect.ValueOf(value))
	if src == decimalType {
		return decimal.NewFromString(str)
	}
	rv, err := convertOp1(str, src)
	if err != nil {
		return nil, err
	}
	return rv.Interface(), nil
}

// ConvertToString 将支持的基础类型转换为字符串。
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

// Add 对两个同类型数值或 Decimal 执行加法。
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

// ==================== 字节与字符串转换 ====================

// String2Bytes 返回字符串内容的独立字节副本。
func String2Bytes(s string) []byte {
	if s == "" {
		return nil
	}
	return []byte(s)
}

// Bytes2String 返回字节内容的独立字符串副本。
func Bytes2String(b []byte) string {
	return string(b)
}

// IsEqual 报告两个同类型值是否深度相等。
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
