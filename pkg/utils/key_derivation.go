// 本文件提供 PBKDF2 密钥、随机盐值和 JWT key 派生能力。
package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

// DeriveKey 使用旧版 10,000 次 PBKDF2 迭代派生兼容密钥。
func DeriveKey(password, salt string, keyLen int) []byte {
	return pbkdf2.Key([]byte(password), []byte(salt), 10000, keyLen, sha256.New)
}

// GenerateSalt 使用密码学随机源生成指定长度的盐值。
func GenerateSalt(length int) ([]byte, error) {
	salt := make([]byte, length)
	_, err := rand.Read(salt)
	return salt, err
}

// DeriveKeySecure 使用 100,000 次 PBKDF2 迭代派生密钥。
func DeriveKeySecure(password string, salt []byte, keyLen int) []byte {
	const iterations = 100000
	return pbkdf2.Key([]byte(password), salt, iterations, keyLen, sha256.New)
}

// DeriveKeyWithSalt 生成 16 字节随机盐值并派生密钥。
func DeriveKeyWithSalt(password string, keyLen int) ([]byte, []byte, error) {
	salt, err := GenerateSalt(16)
	if err != nil {
		return nil, nil, err
	}
	key := DeriveKeySecure(password, salt, keyLen)
	return key, salt, nil
}

// DeriveJWTKey 使用用户标识参与盐值构造，派生 URL-safe JWT key。
func DeriveJWTKey(password, userID string) string {
	salt := fmt.Sprintf("jwt-salt-%s", userID)
	key := pbkdf2.Key([]byte(password), []byte(salt), 50000, 32, sha256.New)
	return base64.URLEncoding.EncodeToString(key)
}
