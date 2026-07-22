// 本文件提供整数、浮点数和可空数字文本校验。
package utils

import "strconv"

// IsInteger 报告文本能否解析为整数。
// 注意：如果字符串是浮点数形式的，也会返回true
func IsInteger(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

// IsFloat 报告文本能否解析为浮点数。
// 注意：如果字符串是整数形式的，也会返回true
// 例如 "123" 会被认为是浮点数
func IsFloat(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// IsNumber 报告文本能否解析为整数或浮点数。
func IsNumber(s string) bool {
	return IsInteger(s) || IsFloat(s)
}

// IsNumberOrNil 报告文本为空或能否解析为数字。
func IsNumberOrNil(s string) bool {
	if s == "" {
		return true
	}
	return IsNumber(s)
}

// IsNumberOrNilInt 报告文本为空或能否解析为整数。
func IsNumberOrNilInt(s string) bool {
	if s == "" {
		return true
	}
	_, err := strconv.Atoi(s)
	return err == nil
}

// IsNumberOrNilFloat 报告文本为空或能否解析为浮点数。
func IsNumberOrNilFloat(s string) bool {
	if s == "" {
		return true
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
