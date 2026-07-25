// 本文件提供 Unicode 安全的首字符转换和 rune 辅助函数。
package utils

import (
	"unicode"
	"unicode/utf8"
)

// FirstUpper 将字符串的第一个 Unicode 字符转换为大写。
func FirstUpper(s string) string {
	first, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return ""
	}
	return string(unicode.ToUpper(first)) + s[size:]
}

// FirstLower 将字符串的第一个 Unicode 字符转换为小写。
func FirstLower(s string) string {
	first, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return ""
	}
	return string(unicode.ToLower(first)) + s[size:]
}

// TrimFirstRune 删除字符串的第一个 Unicode 字符。
func TrimFirstRune(s string) string {
	_, size := utf8.DecodeRuneInString(s)
	return s[size:]
}
