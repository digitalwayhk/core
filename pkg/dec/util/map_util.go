package util

import (
	"reflect"
	"strconv"
)

var MapUtil = mapUtil{}

type mapUtil struct {
}

func (u mapUtil) GetString(m map[string]interface{}, key string) string {
	value := u.GetValue(m, key)
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return JsonUtil.ToString(value)
}

func (u mapUtil) GetInt64(m map[string]interface{}, key string, defaultValue int64) int64 {
	value := u.GetValue(m, key)
	if value == nil {
		return defaultValue
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(v.Uint())
	case reflect.Float32, reflect.Float64:
		return int64(v.Float())
	case reflect.String:
		result, err := strconv.ParseInt(v.String(), 10, 64)
		if err != nil {
			return defaultValue
		}
		return result
	}

	return defaultValue
}

func (u mapUtil) GetUint64(m map[string]interface{}, key string, defaultValue uint64) uint64 {
	value := u.GetValue(m, key)
	if value == nil {
		return defaultValue
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return uint64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Float32, reflect.Float64:
		return uint64(v.Float())
	case reflect.String:
		result, err := strconv.ParseUint(v.String(), 10, 64)
		if err != nil {
			return defaultValue
		}
		return result
	}
	return defaultValue
}

func (u mapUtil) GetFloat64(m map[string]interface{}, key string, defaultValue float64) float64 {
	value := u.GetValue(m, key)
	if value == nil {
		return defaultValue
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint())
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.String:
		result, err := strconv.ParseFloat(v.String(), 64)
		if err != nil {
			return defaultValue
		}
		return result
	}
	return defaultValue
}

func (u mapUtil) GetBool(m map[string]interface{}, key string, defaultValue bool) bool {
	value := u.GetValue(m, key)
	if value == nil {
		return defaultValue
	}
	if s, ok := value.(bool); ok {
		return s
	}
	if s, ok := value.(string); ok {
		result, err := strconv.ParseBool(s)
		if err != nil {
			return defaultValue
		}
		return result
	}
	return defaultValue
}

func (mapUtil) GetValue(m map[string]interface{}, key string) interface{} {
	if m == nil {
		return nil
	}
	if value, ok := m[key]; ok {
		return value
	}
	return nil
}
