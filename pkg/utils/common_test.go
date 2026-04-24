package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===== Md5 =====

func TestMd5_Single(t *testing.T) {
	result := Md5("hello")
	assert.Len(t, result, 32)
	assert.Equal(t, result, Md5("hello"), "same input should produce same hash")
}

func TestMd5_Multiple(t *testing.T) {
	r1 := Md5("hello", "world")
	r2 := Md5("helloworld")
	assert.Equal(t, r1, r2, "concatenated strings should produce same md5")
}

func TestMd5_Empty(t *testing.T) {
	result := Md5("")
	assert.Len(t, result, 32)
}

// ===== Hash functions =====

func TestShortHash(t *testing.T) {
	h := ShortHash("test")
	assert.Len(t, h, 16)
	assert.Equal(t, h, ShortHash("test"))
}

func TestMediumHash(t *testing.T) {
	h := MediumHash("test")
	assert.Len(t, h, 32)
	assert.Equal(t, h, MediumHash("test"))
}

func TestSecureHash(t *testing.T) {
	h := SecureHash("test")
	assert.Len(t, h, 64)
	assert.Equal(t, h, SecureHash("test"))
}

func TestHashCodeHex_BackwardCompat(t *testing.T) {
	// HashCodeHex is an alias for SecureHash
	assert.Equal(t, SecureHash("abc"), HashCodeHex("abc"))
}

func TestHashCode64(t *testing.T) {
	h1 := HashCode64("test")
	h2 := HashCode64("test")
	assert.Equal(t, h1, h2)
	assert.NotEqual(t, HashCode64("a"), HashCode64("b"))
}

func TestHashCodes(t *testing.T) {
	h := HashCodes("a", "b", "c")
	assert.Len(t, h, 32)
	assert.Equal(t, h, HashCodes("a", "b", "c"))
	assert.NotEqual(t, HashCodes("a", "b"), HashCodes("b", "a"))
}

func TestUserIDHash(t *testing.T) {
	h := UserIDHash("user123")
	assert.Len(t, h, 32)
	assert.Equal(t, h, UserIDHash("user123"))
	assert.NotEqual(t, UserIDHash("user1"), UserIDHash("user2"))
}

func TestUserIDUUID(t *testing.T) {
	id := UserIDUUID("user123")
	// UUID format: 8-4-4-4-12 = 36 chars
	assert.Len(t, id, 36)
	assert.Equal(t, id, UserIDUUID("user123"))
	assert.NotEqual(t, UserIDUUID("user1"), UserIDUUID("user2"))
}

// ===== ToTime =====

func TestToTime(t *testing.T) {
	// 1609459200000 ms = 2021-01-01 00:00:00 UTC
	ts := "1609459200000"
	tm := ToTime(ts)
	assert.Equal(t, 2021, tm.Year())
	assert.Equal(t, 1, int(tm.Month()))
}

func TestToTime_Zero(t *testing.T) {
	tm := ToTime("0")
	assert.Equal(t, 1970, tm.Year())
}

// ===== FirstUpper / FirstLower =====

func TestFirstUpper(t *testing.T) {
	assert.Equal(t, "Hello", FirstUpper("hello"))
	assert.Equal(t, "Hello", FirstUpper("Hello"))
	assert.Equal(t, "A", FirstUpper("a"))
	assert.Equal(t, "", FirstUpper(""))
}

func TestFirstLower(t *testing.T) {
	assert.Equal(t, "hello", FirstLower("Hello"))
	assert.Equal(t, "hello", FirstLower("hello"))
	assert.Equal(t, "a", FirstLower("A"))
	assert.Equal(t, "", FirstLower(""))
}

// ===== IsEmail =====

func TestIsEmail_Valid(t *testing.T) {
	cases := []string{
		"user@example.com",
		"user.name+tag@domain.co",
		"test@test.org",
	}
	for _, c := range cases {
		assert.True(t, IsEmail(c), "expected %q to be valid email", c)
	}
}

func TestIsEmail_Invalid(t *testing.T) {
	cases := []string{
		"notanemail",
		"missing@",
		"@nodomain",
		"",
	}
	for _, c := range cases {
		assert.False(t, IsEmail(c), "expected %q to be invalid email", c)
	}
}

// ===== IsMobile =====

func TestIsMobile_Valid(t *testing.T) {
	assert.True(t, IsMobile("+86 13800138000"))
	assert.True(t, IsMobile("+1 5551234567"))
}

func TestIsMobile_Invalid(t *testing.T) {
	assert.False(t, IsMobile("13800138000"))   // no space
	assert.False(t, IsMobile("+86 abc123"))    // non-numeric phone
	assert.False(t, IsMobile(""))
}

// ===== TrimFirstRune =====

func TestTrimFirstRune(t *testing.T) {
	assert.Equal(t, "ello", TrimFirstRune("hello"))
	assert.Equal(t, "", TrimFirstRune("h"))
	// Multi-byte rune
	assert.Equal(t, "世界", TrimFirstRune("你世界"))
}

// ===== PrintObj =====

func TestPrintObj_Struct(t *testing.T) {
	type S struct{ Name string }
	s := PrintObj(S{Name: "test"})
	assert.Contains(t, s, "test")
}

func TestPrintObj_Slice(t *testing.T) {
	items := []string{"a", "b"}
	result := PrintObj(items)
	// For a slice, the last element's JSON is returned
	_ = result // just ensure no panic
}

// ===== GetRandNum =====

func TestGetRandNum(t *testing.T) {
	for i := 0; i < 10; i++ {
		n := GetRandNum(100)
		assert.GreaterOrEqual(t, n, 0)
		assert.Less(t, n, 100)
	}
}

// ===== IsTest =====

func TestIsTest(t *testing.T) {
	// When running under go test, IsTest should return true
	assert.True(t, IsTest())
}

// ===== PrintObj with map =====

func TestPrintObj_Map(t *testing.T) {
	m := map[string]int{"key": 1}
	result := PrintObj(m)
	require.NotEmpty(t, result)
}
