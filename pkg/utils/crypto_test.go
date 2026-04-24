package utils

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===== PaddingText / UnPaddingText =====

func TestPaddingUnPadding_RoundTrip(t *testing.T) {
	plaintext := []byte("hello world")
	blockSize := 8
	padded := PaddingText(plaintext, blockSize)
	assert.Equal(t, 0, len(padded)%blockSize)
	unpadded := UnPaddingText(padded)
	assert.Equal(t, plaintext, unpadded)
}

func TestPaddingText_AlreadyAligned(t *testing.T) {
	// 8-byte block, 8-byte input: padding adds a full block
	input := []byte("12345678")
	padded := PaddingText(input, 8)
	assert.Equal(t, 16, len(padded))
	unpadded := UnPaddingText(padded)
	assert.Equal(t, input, unpadded)
}

// ===== DES encrypt/decrypt =====

func TestDES_EncryptDecrypt(t *testing.T) {
	key := []byte("abcdefgh") // DES key must be 8 bytes
	plaintext := []byte("hello world DES!")
	encrypted := EncyptogDES(plaintext, key)
	decrypted := DecrptogDES(encrypted, key)
	assert.Equal(t, plaintext, decrypted)
}

func TestDES_DifferentPlaintexts(t *testing.T) {
	key := []byte("testkey1")
	p1 := EncyptogDES([]byte("first message"), key)
	p2 := EncyptogDES([]byte("second message!"), key)
	assert.NotEqual(t, p1, p2)
}

// ===== 3DES encrypt/decrypt =====

func TestTripleDES_EncryptDecrypt(t *testing.T) {
	// 3DES key must be 24 bytes
	key := []byte("abcdefghijklmnopqrstuvwx")
	plaintext := []byte("hello 3DES world!")
	encrypted := Encyptog3DES(plaintext, key)
	decrypted := Decrptog3DES(encrypted, key)
	assert.Equal(t, plaintext, decrypted)
}

// ===== AES encrypt/decrypt =====

func TestAES_EncryptDecrypt(t *testing.T) {
	key := "my-secret-key"
	plaintext := "Hello, AES encryption!"
	ciphertext, err := EncryptAES(plaintext, key)
	require.NoError(t, err)
	assert.NotEmpty(t, ciphertext)
	assert.NotEqual(t, plaintext, ciphertext)

	decrypted, err := DecryptAES(ciphertext, key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestAES_WrongKey(t *testing.T) {
	ciphertext, err := EncryptAES("secret", "correct-key")
	require.NoError(t, err)
	_, err = DecryptAES(ciphertext, "wrong-key")
	assert.Error(t, err)
}

func TestAES_EmptyPlaintext(t *testing.T) {
	key := "somekey"
	cipher, err := EncryptAES("", key)
	require.NoError(t, err)
	decrypted, err := DecryptAES(cipher, key)
	require.NoError(t, err)
	assert.Equal(t, "", decrypted)
}

func TestDecryptAES_InvalidBase64(t *testing.T) {
	_, err := DecryptAES("not-base64!!!", "key")
	assert.Error(t, err)
}

func TestDecryptAES_TooShort(t *testing.T) {
	// Valid base64 but ciphertext too short for nonce
	import64 := "YQ==" // base64 of "a" (1 byte)
	_, err := DecryptAES(import64, "key")
	assert.Error(t, err)
}

// ===== DeriveKey =====

func TestDeriveKey(t *testing.T) {
	key := DeriveKey("password", "salt", 32)
	assert.Len(t, key, 32)
	// Deterministic
	assert.Equal(t, key, DeriveKey("password", "salt", 32))
	// Different password -> different key
	other := DeriveKey("other-password", "salt", 32)
	assert.NotEqual(t, key, other)
}

// ===== GenerateSalt =====

func TestGenerateSalt(t *testing.T) {
	salt, err := GenerateSalt(16)
	require.NoError(t, err)
	assert.Len(t, salt, 16)

	salt2, err := GenerateSalt(16)
	require.NoError(t, err)
	// Two random salts should (almost certainly) differ
	assert.NotEqual(t, salt, salt2)
}

// ===== DeriveKeySecure =====

func TestDeriveKeySecure(t *testing.T) {
	salt := []byte("random-salt-1234")
	key := DeriveKeySecure("password", salt, 32)
	assert.Len(t, key, 32)
	assert.Equal(t, key, DeriveKeySecure("password", salt, 32))
}

// ===== DeriveKeyWithSalt =====

func TestDeriveKeyWithSalt(t *testing.T) {
	key, salt, err := DeriveKeyWithSalt("password", 32)
	require.NoError(t, err)
	assert.Len(t, key, 32)
	assert.Len(t, salt, 16)
}

// ===== DeriveJWTKey =====

func TestDeriveJWTKey(t *testing.T) {
	k1 := DeriveJWTKey("password", "user1")
	k2 := DeriveJWTKey("password", "user2")
	assert.NotEmpty(t, k1)
	assert.NotEqual(t, k1, k2)
	// Deterministic
	assert.Equal(t, k1, DeriveJWTKey("password", "user1"))
	// Base64 URL encoded
	assert.False(t, strings.ContainsAny(k1, "+/"))
}
