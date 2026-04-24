package utils

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===== Test structs =====

type personSource struct {
	Name string
	Age  int
	City string
}

type personTarget struct {
	Name string
	Age  int
	City string
}

type personPartial struct {
	Name string
}

// ===== IsEqual =====

func TestIsEqual_SameValues(t *testing.T) {
	assert.True(t, IsEqual(42, 42))
	assert.True(t, IsEqual("hello", "hello"))
	assert.True(t, IsEqual(3.14, 3.14))
}

func TestIsEqual_DifferentValues(t *testing.T) {
	assert.False(t, IsEqual(1, 2))
	assert.False(t, IsEqual("a", "b"))
}

func TestIsEqual_DifferentTypes(t *testing.T) {
	assert.False(t, IsEqual(1, int64(1)))
	assert.False(t, IsEqual(1, "1"))
}

func TestIsEqual_Nils(t *testing.T) {
	assert.True(t, IsEqual(nil, nil))
	assert.False(t, IsEqual(nil, 1))
	assert.False(t, IsEqual(1, nil))
}

// ===== String2Bytes / Bytes2String =====

func TestString2Bytes(t *testing.T) {
	s := "hello world"
	b := String2Bytes(s)
	assert.Equal(t, []byte(s), b)
}

func TestString2Bytes_Empty(t *testing.T) {
	assert.Nil(t, String2Bytes(""))
}

func TestBytes2String(t *testing.T) {
	b := []byte("test bytes")
	s := Bytes2String(b)
	assert.Equal(t, "test bytes", s)
}

func TestBytes2String_Empty(t *testing.T) {
	assert.Equal(t, "", Bytes2String(nil))
	assert.Equal(t, "", Bytes2String([]byte{}))
}

// ===== IsPtr =====

func TestIsPtr(t *testing.T) {
	s := "hello"
	assert.True(t, IsPtr(&s))
	assert.False(t, IsPtr(s))
	assert.False(t, IsPtr(42))
}

// ===== GetTypeName =====

func TestGetTypeName(t *testing.T) {
	assert.Equal(t, "personSource", GetTypeName(personSource{}))
	assert.Equal(t, "personSource", GetTypeName(&personSource{}))
}

// ===== HasProperty =====

func TestHasProperty_Exists(t *testing.T) {
	assert.True(t, HasProperty(personSource{}, "Name"))
	assert.True(t, HasProperty(&personSource{}, "Age"))
}

func TestHasProperty_NotExists(t *testing.T) {
	assert.False(t, HasProperty(personSource{}, "Unknown"))
}

// ===== GetPropertyType =====

func TestGetPropertyType_Found(t *testing.T) {
	sf := GetPropertyType(personSource{}, "Name")
	require.NotNil(t, sf)
	assert.Equal(t, "Name", sf.Name)
}

func TestGetPropertyType_NotFound(t *testing.T) {
	sf := GetPropertyType(personSource{}, "Missing")
	assert.Nil(t, sf)
}

// ===== GetPropertyValue =====

func TestGetPropertyValue(t *testing.T) {
	p := personSource{Name: "Alice", Age: 30}
	assert.Equal(t, "Alice", GetPropertyValue(&p, "Name"))
	assert.Equal(t, 30, GetPropertyValue(&p, "Age"))
}

// ===== SetPropertyValue =====

func TestSetPropertyValue(t *testing.T) {
	p := &personSource{Name: "old", Age: 0}
	err := SetPropertyValue(p, "Name", "new")
	require.NoError(t, err)
	assert.Equal(t, "new", p.Name)
}

func TestSetPropertyValue_TypeConversion(t *testing.T) {
	p := &personSource{Age: 0}
	err := SetPropertyValue(p, "Age", 42)
	require.NoError(t, err)
	assert.Equal(t, 42, p.Age)
}

// ===== IsArray =====

func TestIsArray(t *testing.T) {
	assert.True(t, IsArray([]int{1, 2, 3}))
	assert.True(t, IsArray([3]int{1, 2, 3}))
	assert.False(t, IsArray("not an array"))
	assert.False(t, IsArray(nil))
	assert.False(t, IsArray(42))
}

// ===== ArrayEach =====

func TestArrayEach(t *testing.T) {
	sum := 0
	ArrayEach([]int{1, 2, 3, 4}, func(item interface{}) {
		sum += item.(int)
	})
	assert.Equal(t, 10, sum)
}

func TestArrayEach_NonArray(t *testing.T) {
	// Should not panic
	called := 0
	ArrayEach("not-array", func(item interface{}) { called++ })
	assert.Equal(t, 0, called)
}

// ===== GetTypeKind =====

func TestGetTypeKind(t *testing.T) {
	assert.Equal(t, Base, GetTypeKind(reflect.TypeOf(42)))
	assert.Equal(t, Base, GetTypeKind(reflect.TypeOf("str")))
	assert.Equal(t, Array, GetTypeKind(reflect.TypeOf([]int{})))
	assert.Equal(t, Ptr, GetTypeKind(reflect.TypeOf(new(int))))
	assert.Equal(t, Struct, GetTypeKind(reflect.TypeOf(personSource{})))
	assert.Equal(t, Func, GetTypeKind(reflect.TypeOf(func() {})))
}

// ===== AutoMapConvert =====

func TestAutoMapConvert_Basic(t *testing.T) {
	src := &personSource{Name: "Bob", Age: 25, City: "HK"}
	dst := &personTarget{}
	AutoMapConvert(src, dst, nil)
	assert.Equal(t, "Bob", dst.Name)
	assert.Equal(t, 25, dst.Age)
	assert.Equal(t, "HK", dst.City)
}

func TestAutoMapConvert_PartialTarget(t *testing.T) {
	src := &personSource{Name: "Carol", Age: 40}
	dst := &personPartial{}
	AutoMapConvert(src, dst, nil)
	assert.Equal(t, "Carol", dst.Name)
}

func TestAutoMapConvert_WithCustomHandler(t *testing.T) {
	src := &personSource{Name: "Dave", Age: 20}
	dst := &personTarget{}
	AutoMapConvert(src, dst, func(arge *AutoMapArge) *AutoMapHander {
		if arge.Field == "Name" {
			return &AutoMapHander{TargetValue: "Overridden"}
		}
		return nil
	})
	assert.Equal(t, "Overridden", dst.Name)
	assert.Equal(t, 20, dst.Age)
}

func TestAutoMapConvert_Slice(t *testing.T) {
	srcs := []personSource{
		{Name: "A", Age: 1},
		{Name: "B", Age: 2},
	}
	result := AutoMapConvert(srcs, personTarget{}, nil)
	items, ok := result.([]interface{})
	require.True(t, ok)
	assert.Len(t, items, 2)
}

// ===== ConvertToString =====

func TestConvertToString(t *testing.T) {
	assert.Equal(t, "42", ConvertToString(42))
	assert.Equal(t, "3.14", ConvertToString(3.14))
	assert.Equal(t, "true", ConvertToString(true))
	assert.Equal(t, "hello", ConvertToString("hello"))
}

// ===== AnyToTypeData =====

func TestAnyToTypeData(t *testing.T) {
	v, err := AnyToTypeData("42", reflect.TypeOf(0))
	require.NoError(t, err)
	assert.Equal(t, 42, v)
}

func TestAnyToTypeData_Nil(t *testing.T) {
	v, err := AnyToTypeData(nil, reflect.TypeOf(0))
	require.NoError(t, err)
	assert.Nil(t, v)
}

// ===== NewInterface / NewInterfaceByType =====

func TestNewInterface(t *testing.T) {
	obj := NewInterface(personSource{})
	require.NotNil(t, obj)
	_, ok := obj.(*personSource)
	assert.True(t, ok)
}

// ===== RecycleObject =====

func TestRecycleObject_NocrashOnNil(t *testing.T) {
	// Should not panic
	RecycleObject(nil)
}

func TestRecycleObject(t *testing.T) {
	p := &personSource{Name: "temp"}
	// Get once to populate pool
	_ = NewInterface(personSource{})
	// Recycle should not panic
	RecycleObject(p)
}

// ===== DeepForItem =====

func TestDeepForItem_Nil(t *testing.T) {
	// Should not panic
	DeepForItem(nil, func(field, parent reflect.StructField, kind TypeKind) {})
}

func TestDeepForItem_Struct(t *testing.T) {
	fields := []string{}
	DeepForItem(personSource{}, func(field, parent reflect.StructField, kind TypeKind) {
		fields = append(fields, field.Name)
	})
	assert.Contains(t, fields, "Name")
	assert.Contains(t, fields, "Age")
	assert.Contains(t, fields, "City")
}

// ===== ForEach =====

func TestForEach(t *testing.T) {
	p := &personSource{Name: "Eve", Age: 30}
	seen := map[string]interface{}{}
	ForEach(p, func(name string, value interface{}) {
		seen[name] = value
	})
	assert.Equal(t, "Eve", seen["Name"])
	assert.Equal(t, 30, seen["Age"])
}

// ===== Add =====

func TestAdd_Int(t *testing.T) {
	v1 := reflect.ValueOf(3)
	v2 := reflect.ValueOf(4)
	result := Add(v1, v2)
	assert.Equal(t, 7, result.Interface())
}

func TestAdd_Float(t *testing.T) {
	v1 := reflect.ValueOf(1.5)
	v2 := reflect.ValueOf(2.5)
	result := Add(v1, v2)
	assert.InDelta(t, 4.0, result.Interface(), 0.0001)
}

// ===== GetPropertyTypeByElemName =====

type containerStruct struct {
	Items []*personSource
}

func TestGetPropertyTypeByElemName_Found(t *testing.T) {
	sf := GetPropertyTypeByElemName(containerStruct{}, "personSource")
	require.NotNil(t, sf)
	assert.Equal(t, "Items", sf.Name)
}

func TestGetPropertyTypeByElemName_NotFound(t *testing.T) {
	sf := GetPropertyTypeByElemName(containerStruct{}, "NoSuchType")
	assert.Nil(t, sf)
}

func TestGetPropertyTypeByElemName_NonStruct(t *testing.T) {
	sf := GetPropertyTypeByElemName("not a struct", "anything")
	assert.Nil(t, sf)
}
