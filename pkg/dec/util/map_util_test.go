package util

import (
	"fmt"
	"strconv"
	"testing"
)

func TestGetString(t *testing.T) {
	i := "100"
	ii, _ := strconv.Atoi(i)
	m := initNumberMap(ii)
	m["String"] = i
	m["Bool"] = true
	fmt.Printf("==========GetString Result1==========\n")
	for k, _ := range m {
		v := MapUtil.GetString(m, k)
		fmt.Printf("%s=%v[%T]\n", k, v, v)
		if k != "Bool" && v != i {
			t.Fail()
		}
		if k == "Bool" && v != "true" {
			t.Fail()
		}
	}
}

func TestGetInt64(t *testing.T) {
	i := int64(100)
	defaultValue := int64(20)
	m := initNumberMap(int(i))
	m["String"] = strconv.Itoa(int(i))
	m["Bool"] = true
	fmt.Printf("==========TestGetInt64 Result==========\n")
	for k, _ := range m {
		v := MapUtil.GetInt64(m, k, defaultValue)
		fmt.Printf("%s=%v[%T]\n", k, v, v)
		if k != "Bool" && v != i {
			t.Fail()
		}
		if k == "Bool" && v != defaultValue {
			t.Fail()
		}
	}
}

func TestGetUint64(t *testing.T) {
	i := uint64(100)
	defaultValue := uint64(20)
	m := initNumberMap(int(i))
	m["String"] = strconv.Itoa(int(i))
	m["Bool"] = true
	fmt.Printf("==========TestGetUint64 Result==========\n")
	for k, _ := range m {
		v := MapUtil.GetUint64(m, k, defaultValue)
		fmt.Printf("%s=%v[%T]\n", k, v, v)
		if k != "Bool" && v != i {
			t.Fail()
		}
		if k == "Bool" && v != defaultValue {
			t.Fail()
		}
	}
}

func TestGetFloat64(t *testing.T) {
	i := float64(100)
	defaultValue := float64(20)
	m := initNumberMap(int(i))
	m["String"] = strconv.Itoa(int(i))
	m["Bool"] = true
	fmt.Printf("==========TestGetFloat64 Result==========\n")
	for k, _ := range m {
		v := MapUtil.GetFloat64(m, k, defaultValue)
		fmt.Printf("%s=%v[%T]\n", k, v, v)
		if k != "Bool" && v != i {
			t.Fail()
		}
		if k == "Bool" && v != defaultValue {
			t.Fail()
		}
	}
}

func TestGetBool(t *testing.T) {
	i := 100
	defaultValue := false
	m := initNumberMap(i)
	m["String"] = strconv.Itoa(i)
	m["Bool"] = true
	fmt.Printf("==========TestGetBool Result==========\n")
	for k, _ := range m {
		v := MapUtil.GetBool(m, k, defaultValue)
		fmt.Printf("%s=%v[%T]\n", k, v, v)
		if k != "Bool" && v {
			t.Fail()
		}
		if k == "Bool" && !v {
			t.Fail()
		}
	}
}

func TestA(t *testing.T) {
	s := "alter table fleet_order_service_plan_tab_%08d modify column sub_district varchar(256);\n"
	for i := 0; i < 1000; i++ {
		fmt.Printf(s, i)
	}
}
