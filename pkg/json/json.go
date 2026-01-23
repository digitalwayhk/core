package json

import (
	jsoniter "github.com/json-iterator/go"
)

var (
	// 使用兼容标准库的配置
	json = jsoniter.ConfigCompatibleWithStandardLibrary

	// 或使用最快配置 (可能有兼容性问题)
	// json = jsoniter.ConfigFastest
)

// Marshal 替代 encoding/json.Marshal
func Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal 替代 encoding/json.Unmarshal
func Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// MarshalToString 直接返回字符串
func MarshalToString(v interface{}) (string, error) {
	return json.MarshalToString(v)
}

// UnmarshalFromString 从字符串解析
func UnmarshalFromString(str string, v interface{}) error {
	return json.UnmarshalFromString(str, v)
}
