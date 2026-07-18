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

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
