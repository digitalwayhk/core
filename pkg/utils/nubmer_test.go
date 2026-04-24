package utils

import "testing"

func TestIsInteger(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		"0":     true,
		"123":   true,
		"-42":   true,
		"12.3":  false,
		"abc":   false,
		" 1":    false,
	}
	for in, want := range cases {
		if got := IsInteger(in); got != want {
			t.Errorf("IsInteger(%q)=%v want %v", in, got, want)
		}
	}
}

func TestIsFloat(t *testing.T) {
	cases := map[string]bool{
		"":     false,
		"0":    true, // integer is also valid float (per docs)
		"3.14": true,
		"-2.5": true,
		"1e3":  true,
		"abc":  false,
	}
	for in, want := range cases {
		if got := IsFloat(in); got != want {
			t.Errorf("IsFloat(%q)=%v want %v", in, got, want)
		}
	}
}

func TestIsNumber(t *testing.T) {
	if !IsNumber("123") {
		t.Error("123 should be a number")
	}
	if !IsNumber("12.3") {
		t.Error("12.3 should be a number")
	}
	if IsNumber("abc") {
		t.Error("abc should not be a number")
	}
	if IsNumber("") {
		t.Error("empty string should not be a number")
	}
}

func TestIsNumberOrNil(t *testing.T) {
	if !IsNumberOrNil("") {
		t.Error("empty should be allowed")
	}
	if !IsNumberOrNil("12.3") {
		t.Error("12.3 should pass")
	}
	if IsNumberOrNil("abc") {
		t.Error("abc should fail")
	}
}

func TestIsNumberOrNilInt(t *testing.T) {
	if !IsNumberOrNilInt("") {
		t.Error("empty should pass")
	}
	if !IsNumberOrNilInt("42") {
		t.Error("42 should pass")
	}
	if IsNumberOrNilInt("12.3") {
		t.Error("12.3 should fail (not an int)")
	}
	if IsNumberOrNilInt("abc") {
		t.Error("abc should fail")
	}
}

func TestIsNumberOrNilFloat(t *testing.T) {
	if !IsNumberOrNilFloat("") {
		t.Error("empty should pass")
	}
	if !IsNumberOrNilFloat("3.14") {
		t.Error("3.14 should pass")
	}
	if IsNumberOrNilFloat("abc") {
		t.Error("abc should fail")
	}
}
