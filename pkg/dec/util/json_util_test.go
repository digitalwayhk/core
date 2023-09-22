package util

import (
	"fmt"
	"reflect"
	"testing"
)

func TestParseMap(t *testing.T) {
	i := 100
	m := initNumberMap(i)
	s := JsonUtil.ToString(m)
	fmt.Println("json string:" + s)
	mm, err := JsonUtil.ParseMap(s)
	if err != nil {
		fmt.Printf("TestParseMap fail,value:%s,err:%v\n", s, err)
		t.Fail()
		return
	}
	fmt.Printf("==========ParseMap Result==========\n")
	for k, v := range mm {
		fmt.Printf("%s=%v[%T]\n", k, v, v)
		if reflect.ValueOf(v).Kind() != reflect.Float64 {
			t.Fail()
		}
	}
}

func TestParseStringList(t *testing.T) {
	l := []string{"123", "adf", "333"}
	s := JsonUtil.ToString(l)
	fmt.Println("json string:" + s)
	ll, err := JsonUtil.ParseStringList(s)
	if err != nil {
		fmt.Printf("TestParseStringList fail,value:%s,err:%v\n", s, err)
		t.Fail()
		return
	}
	fmt.Printf("result:%v[%T]\n", ll, ll)
}

func initNumberMap(i int) map[string]interface{} {
	m := make(map[string]interface{})
	m["Byte"] = byte(i)
	m["Int"] = i
	m["Int8"] = int8(i)
	m["Int16"] = int16(i)
	m["Int32"] = int32(i)
	m["Int64"] = int64(i)
	m["Uint"] = uint(i)
	m["Uint8"] = uint8(i)
	m["Uint16"] = uint16(i)
	m["Uint32"] = uint32(i)
	m["Uint64"] = uint64(i)
	m["Float32"] = float32(i)
	m["Float64"] = float64(i)
	return m
}
