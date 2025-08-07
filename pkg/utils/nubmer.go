package utils

import "strconv"

// 是否是整数
// 注意：如果字符串是浮点数形式的，也会返回true
func IsInteger(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

// 是否是浮点数
// 注意：如果字符串是整数形式的，也会返回true
// 例如 "123" 会被认为是浮点数
func IsFloat(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// 是否是数字
func IsNumber(s string) bool {
	return IsInteger(s) || IsFloat(s)
}
func IsNumberOrNil(s string) bool {
	if s == "" {
		return true
	}
	return IsNumber(s)
}
func IsNumberOrNilInt(s string) bool {
	if s == "" {
		return true
	}
	_, err := strconv.Atoi(s)
	return err == nil
}
func IsNumberOrNilFloat(s string) bool {
	if s == "" {
		return true
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
