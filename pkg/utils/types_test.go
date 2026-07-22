// 本文件验证反射、类型转换和兼容入口的稳定行为。
package utils

import (
	"reflect"
	"testing"
)

type NamedStatus int

type deepForNamedStatusFixture struct {
	NamedStatus NamedStatus
}

func TestDeepForVisitsNamedBaseFieldWhenFieldNameMatchesTypeName(t *testing.T) {
	var fields []string
	DeepForItem(&deepForNamedStatusFixture{}, func(field, parent reflect.StructField, kind TypeKind) {
		fields = append(fields, field.Name)
		if field.Name == "NamedStatus" && kind != Base {
			t.Fatalf("NamedStatus 应被识别为基础字段，实际 kind=%v", kind)
		}
	})

	if !containsString(fields, "NamedStatus") {
		t.Fatalf("DeepForItem 应枚举字段名与类型名相同的基础字段，实际 fields=%v", fields)
	}
}

func TestAnyToTypeDataPreservesIntegerBaseAutoDetection(t *testing.T) {
	value, err := AnyToTypeData("0x10", reflect.TypeOf(int(0)))
	if err != nil {
		t.Fatalf("AnyToTypeData 应继续支持 base 0 整数字符串: %v", err)
	}
	if value != 16 {
		t.Fatalf("AnyToTypeData(\"0x10\") = %v, want 16", value)
	}
}

func TestRecycleObjectRemainsSourceCompatible(t *testing.T) {
	RecycleObject(&deepForNamedStatusFixture{})
}

func TestBytes2StringReturnsIndependentString(t *testing.T) {
	input := []byte("abc")
	got := Bytes2String(input)
	input[0] = 'z'
	if got != "abc" {
		t.Fatalf("Bytes2String() 与输入共享内存，实际=%q", got)
	}
}

func TestString2BytesReturnsIndependentBytes(t *testing.T) {
	source := []byte{'a', 'b', 'c'}
	input := string(source)
	source[0] = 'q'
	got := String2Bytes(input)
	got[0] = 'z'
	if input != "abc" {
		t.Fatalf("String2Bytes() 修改了输入字符串，实际=%q", input)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
