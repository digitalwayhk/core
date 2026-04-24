package utils

import (
	"bytes"
	"testing"
)

func TestPaddingUnpadding_RoundTrip(t *testing.T) {
	cases := [][]byte{
		[]byte("a"),
		[]byte("hello world"),
		[]byte("12345678"), // exactly block-size
		[]byte(""),
		bytes.Repeat([]byte("x"), 31),
	}
	for _, src := range cases {
		padded := PaddingText(append([]byte(nil), src...), 8)
		if len(padded)%8 != 0 {
			t.Fatalf("PaddingText length not multiple of block size: %d", len(padded))
		}
		out := UnPaddingText(padded)
		if !bytes.Equal(out, src) {
			t.Fatalf("round-trip mismatch: src=%q got=%q", src, out)
		}
	}
}

func TestDES_RoundTrip(t *testing.T) {
	key := []byte("12345678") // 8 bytes for DES
	plaintext := []byte("hello DES, this is a longer message!")
	ct := EncyptogDES(append([]byte(nil), plaintext...), key)
	if bytes.Equal(ct, plaintext) {
		t.Fatal("ciphertext equals plaintext")
	}
	pt := DecrptogDES(append([]byte(nil), ct...), key)
	if !bytes.Equal(pt, plaintext) {
		t.Fatalf("DES round-trip mismatch: got=%q want=%q", pt, plaintext)
	}
}

func TestTripleDES_RoundTrip(t *testing.T) {
	key := []byte("123456789012345678901234") // 24 bytes for 3DES
	plaintext := []byte("triple DES message of arbitrary length 12345")
	ct := Encyptog3DES(append([]byte(nil), plaintext...), key)
	pt := Decrptog3DES(append([]byte(nil), ct...), key)
	if !bytes.Equal(pt, plaintext) {
		t.Fatalf("3DES round-trip mismatch: got=%q want=%q", pt, plaintext)
	}
}

func TestAES_RoundTrip(t *testing.T) {
	key := "my-secret-key"
	plaintext := "the quick brown fox jumps over the lazy dog"
	ct, err := EncryptAES(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptAES error: %v", err)
	}
	if ct == plaintext {
		t.Fatal("ciphertext equals plaintext")
	}
	pt, err := DecryptAES(ct, key)
	if err != nil {
		t.Fatalf("DecryptAES error: %v", err)
	}
	if pt != plaintext {
		t.Fatalf("AES round-trip mismatch: got=%q want=%q", pt, plaintext)
	}
}

func TestAES_NonceUniqueness(t *testing.T) {
	key := "k"
	a, err := EncryptAES("payload", key)
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncryptAES("payload", key)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("expected different ciphertexts due to random nonce")
	}
}

func TestAES_DecryptErrors(t *testing.T) {
	if _, err := DecryptAES("!!!not-base64!!!", "k"); err == nil {
		t.Fatal("expected error for invalid base64")
	}
	if _, err := DecryptAES("YQ==", "k"); err == nil { // 1-byte payload, too short for nonce
		t.Fatal("expected error for ciphertext too short")
	}
	// Wrong key should yield error.
	ct, err := EncryptAES("hello", "right")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptAES(ct, "wrong"); err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestDeriveKey(t *testing.T) {
	a := DeriveKey("password", "salt", 32)
	b := DeriveKey("password", "salt", 32)
	c := DeriveKey("password", "different", 32)
	if !bytes.Equal(a, b) {
		t.Fatal("DeriveKey not deterministic")
	}
	if bytes.Equal(a, c) {
		t.Fatal("DeriveKey did not vary with salt")
	}
	if len(a) != 32 {
		t.Fatalf("DeriveKey length = %d", len(a))
	}
}

func TestGenerateSalt(t *testing.T) {
	a, err := GenerateSalt(16)
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateSalt(16)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 16 || len(b) != 16 {
		t.Fatalf("salt length unexpected: %d, %d", len(a), len(b))
	}
	if bytes.Equal(a, b) {
		t.Fatal("two random salts should not match")
	}
}

func TestDeriveKeySecure(t *testing.T) {
	salt := []byte("0123456789abcdef")
	a := DeriveKeySecure("password", salt, 32)
	b := DeriveKeySecure("password", salt, 32)
	if !bytes.Equal(a, b) {
		t.Fatal("DeriveKeySecure not deterministic")
	}
	if len(a) != 32 {
		t.Fatalf("length = %d", len(a))
	}
}

func TestDeriveKeyWithSalt(t *testing.T) {
	key, salt, err := DeriveKeyWithSalt("password", 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 || len(salt) != 16 {
		t.Fatalf("unexpected lengths: key=%d salt=%d", len(key), len(salt))
	}
	// Same password with same salt should reproduce the key.
	got := DeriveKeySecure("password", salt, 32)
	if !bytes.Equal(got, key) {
		t.Fatal("DeriveKeyWithSalt result not reproducible")
	}
}

func TestDeriveJWTKey(t *testing.T) {
	a := DeriveJWTKey("pw", "user1")
	b := DeriveJWTKey("pw", "user1")
	c := DeriveJWTKey("pw", "user2")
	if a != b {
		t.Fatal("DeriveJWTKey not deterministic")
	}
	if a == c {
		t.Fatal("DeriveJWTKey should differ per user")
	}
	if a == "" {
		t.Fatal("DeriveJWTKey returned empty string")
	}
}
