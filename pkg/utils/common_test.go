package utils

import (
	"strings"
	"testing"
	"time"
)

func TestPrintObj_Struct(t *testing.T) {
	type s struct {
		A int
		B string
	}
	got := PrintObj(s{A: 1, B: "x"})
	if !strings.Contains(got, `"A":1`) || !strings.Contains(got, `"B":"x"`) {
		t.Fatalf("unexpected output: %s", got)
	}
}

func TestPrintObj_Slice(t *testing.T) {
	// PrintObj on slice prints each element via fmt.Println; the return value
	// is the last marshaled element (or empty for empty slice). We just make
	// sure it doesn't panic and produces a string.
	_ = PrintObj([]int{1, 2, 3})
	_ = PrintObj([]int{})
}

func TestGetRandNum_InRange(t *testing.T) {
	for i := 0; i < 50; i++ {
		v := GetRandNum(10)
		if v < 0 || v >= 10 {
			t.Fatalf("GetRandNum(10) out of range: %d", v)
		}
	}
}

func TestToTime(t *testing.T) {
	ts := time.Date(2023, 5, 1, 12, 0, 0, 0, time.UTC).UnixMilli()
	got := ToTime(toStr(ts))
	want := time.Unix(ts/1000, 0)
	if !got.Equal(want) {
		t.Fatalf("ToTime mismatch: got=%v want=%v", got, want)
	}
}

func TestToTime_Empty(t *testing.T) {
	got := ToTime("")
	if !got.Equal(time.Unix(0, 0)) {
		t.Fatalf("ToTime(\"\") expected epoch, got %v", got)
	}
}

func toStr(i int64) string {
	// avoid importing strconv in a one-liner
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [32]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = digits[i%10]
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func TestMd5(t *testing.T) {
	got := Md5("abc")
	if got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Fatalf("Md5(abc) = %s", got)
	}
	// concatenation behavior
	if Md5("ab", "c") != Md5("abc") {
		t.Fatalf("Md5 concat behavior unexpected")
	}
}

func TestUserIDHash_Deterministic(t *testing.T) {
	a := UserIDHash("user@example.com")
	b := UserIDHash("user@example.com")
	c := UserIDHash("other@example.com")
	if a != b {
		t.Fatalf("UserIDHash not deterministic")
	}
	if a == c {
		t.Fatalf("UserIDHash collision for distinct inputs")
	}
	if len(a) != 32 {
		t.Fatalf("UserIDHash length = %d, want 32", len(a))
	}
}

func TestUserIDUUID_Deterministic(t *testing.T) {
	a := UserIDUUID("user@example.com")
	b := UserIDUUID("user@example.com")
	if a != b {
		t.Fatalf("UserIDUUID not deterministic")
	}
	if len(a) != 36 {
		t.Fatalf("UserIDUUID length = %d, want 36", len(a))
	}
}

func TestHashFamilyLengths(t *testing.T) {
	if l := len(ShortHash("abc")); l != 16 {
		t.Fatalf("ShortHash len = %d", l)
	}
	if l := len(MediumHash("abc")); l != 32 {
		t.Fatalf("MediumHash len = %d", l)
	}
	if l := len(SecureHash("abc")); l != 64 {
		t.Fatalf("SecureHash len = %d", l)
	}
	if HashCodeHex("abc") != SecureHash("abc") {
		t.Fatalf("HashCodeHex should equal SecureHash")
	}
}

func TestHashCode64Deterministic(t *testing.T) {
	if HashCode64("abc") != HashCode64("abc") {
		t.Fatal("HashCode64 not deterministic")
	}
	if HashCode64("abc") == HashCode64("abd") {
		t.Fatal("HashCode64 collision for distinct inputs")
	}
}

func TestHashCodes(t *testing.T) {
	a := HashCodes("a", "b", "c")
	b := HashCodes("a", "b", "c")
	if a != b {
		t.Fatal("HashCodes not deterministic")
	}
	if len(a) != 32 {
		t.Fatalf("HashCodes length = %d", len(a))
	}
	if HashCodes("a", "b") == HashCodes("ab") {
		t.Fatal("HashCodes should differentiate by separator")
	}
}

func TestIsTest(t *testing.T) {
	if !IsTest() {
		t.Fatal("IsTest() should be true when running under `go test`")
	}
}

func TestFirstUpperLower(t *testing.T) {
	cases := []struct {
		in, upper, lower string
	}{
		{"", "", ""},
		{"a", "A", "a"},
		{"A", "A", "a"},
		{"hello", "Hello", "hello"},
		{"Hello", "Hello", "hello"},
	}
	for _, c := range cases {
		if got := FirstUpper(c.in); got != c.upper {
			t.Errorf("FirstUpper(%q)=%q want %q", c.in, got, c.upper)
		}
		if got := FirstLower(c.in); got != c.lower {
			t.Errorf("FirstLower(%q)=%q want %q", c.in, got, c.lower)
		}
	}
}

func TestIsEmail(t *testing.T) {
	cases := map[string]bool{
		"foo@bar.com":         true,
		"a.b+tag@example.org": true,
		"":                    false,
		"not-an-email":        false,
		"@no-local.com":       false,
		"missing@":            false,
	}
	for in, want := range cases {
		if got := IsEmail(in); got != want {
			t.Errorf("IsEmail(%q)=%v want %v", in, got, want)
		}
	}
}

func TestIsMobile(t *testing.T) {
	cases := map[string]bool{
		"+86 13800138000": true,
		"86 13800138000":  true,
		"13800138000":     false, // no space
		"":                false,
		"+86 abcdefghijk": false, // not numeric
	}
	for in, want := range cases {
		if got := IsMobile(in); got != want {
			t.Errorf("IsMobile(%q)=%v want %v", in, got, want)
		}
	}
}

func TestTrimFirstRune(t *testing.T) {
	if got := TrimFirstRune("hello"); got != "ello" {
		t.Errorf("ASCII: got %q", got)
	}
	if got := TrimFirstRune("中国"); got != "国" {
		t.Errorf("multibyte: got %q", got)
	}
}
