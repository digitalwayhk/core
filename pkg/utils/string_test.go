// 本文件验证字符串辅助函数不会切断 UTF-8 字符。
package utils

import "testing"

func TestFirstUpperHandlesUnicodeRune(t *testing.T) {
	if got := FirstUpper("éclair"); got != "Éclair" {
		t.Fatalf("FirstUpper()=%q, want %q", got, "Éclair")
	}
}

func TestFirstLowerHandlesUnicodeRune(t *testing.T) {
	if got := FirstLower("Éclair"); got != "éclair" {
		t.Fatalf("FirstLower()=%q, want %q", got, "éclair")
	}
}
