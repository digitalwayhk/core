package util

import (
	"encoding/json"
	"github.com/zeromicro/go-zero/core/logx"
	"reflect"
)

var JsonUtil = jsonUtil{}

type jsonUtil struct {
}

func (jsonUtil) ToString(value interface{}) string {
	if value == nil || reflect.TypeOf(value) == nil {
		return ""
	}
	if v, ok := value.(string); ok {
		return v
	}
	s, err := json.Marshal(value)
	if err != nil {
		logx.Errorf("JsonUtil.ToString error,value=%v,err=%v\n", value, err)
		return ""
	}
	return string(s)
}

func (jsonUtil) ParseStringList(s string) ([]string, error) {
	value := make([]string, 0)
	if s == "" {
		return value, nil
	}
	err := json.Unmarshal([]byte(s), &value)
	return value, err
}

func (jsonUtil) ParseMap(s string) (map[string]interface{}, error) {
	value := make(map[string]interface{})
	if s == "" {
		return value, nil
	}
	err := json.Unmarshal([]byte(s), &value)
	return value, err
}
