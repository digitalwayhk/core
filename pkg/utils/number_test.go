package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===== IsInteger =====

func TestIsInteger_Valid(t *testing.T) {
	cases := []string{"0", "1", "-1", "999", "123456"}
	for _, c := range cases {
		assert.True(t, IsInteger(c), "expected %q to be integer", c)
	}
}

func TestIsInteger_Invalid(t *testing.T) {
	cases := []string{"1.5", "abc", "", "1e5"}
	for _, c := range cases {
		assert.False(t, IsInteger(c), "expected %q to not be integer", c)
	}
}

// ===== IsFloat =====

func TestIsFloat_Valid(t *testing.T) {
	cases := []string{"0.0", "1.5", "-3.14", "100", "1e5", "0"}
	for _, c := range cases {
		assert.True(t, IsFloat(c), "expected %q to be float", c)
	}
}

func TestIsFloat_Invalid(t *testing.T) {
	cases := []string{"abc", "", "1.2.3"}
	for _, c := range cases {
		assert.False(t, IsFloat(c), "expected %q to not be float", c)
	}
}

// ===== IsNumber =====

func TestIsNumber_Valid(t *testing.T) {
	cases := []string{"0", "42", "-7", "3.14", "1e10"}
	for _, c := range cases {
		assert.True(t, IsNumber(c), "expected %q to be a number", c)
	}
}

func TestIsNumber_Invalid(t *testing.T) {
	cases := []string{"hello", "", "12abc"}
	for _, c := range cases {
		assert.False(t, IsNumber(c), "expected %q not to be a number", c)
	}
}

// ===== IsNumberOrNil =====

func TestIsNumberOrNil(t *testing.T) {
	assert.True(t, IsNumberOrNil(""))
	assert.True(t, IsNumberOrNil("42"))
	assert.True(t, IsNumberOrNil("3.14"))
	assert.False(t, IsNumberOrNil("abc"))
}

// ===== IsNumberOrNilInt =====

func TestIsNumberOrNilInt(t *testing.T) {
	assert.True(t, IsNumberOrNilInt(""))
	assert.True(t, IsNumberOrNilInt("10"))
	assert.False(t, IsNumberOrNilInt("3.14"))
	assert.False(t, IsNumberOrNilInt("abc"))
}

// ===== IsNumberOrNilFloat =====

func TestIsNumberOrNilFloat(t *testing.T) {
	assert.True(t, IsNumberOrNilFloat(""))
	assert.True(t, IsNumberOrNilFloat("3.14"))
	assert.True(t, IsNumberOrNilFloat("100"))
	assert.False(t, IsNumberOrNilFloat("abc"))
}


