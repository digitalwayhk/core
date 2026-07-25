// 本文件提供兼容的 MD5、SHA-256、用户标识和组合哈希函数。
package utils

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

// Md5 返回所有参数直接拼接后的 MD5 十六进制摘要。
func Md5(strs ...string) string {
	str := ""
	for _, s := range strs {
		str += s
	}
	data := []byte(str)
	has := md5.Sum(data)
	md5str := fmt.Sprintf("%x", has)
	return md5str
}

// UserIDHash 返回外部用户标识的 32 字符哈希。
func UserIDHash(externalID string) string {
	hash := sha256.Sum256([]byte(externalID))
	return hex.EncodeToString(hash[:16])
}

// UserIDUUID 返回由外部用户标识稳定生成的 UUID。
func UserIDUUID(externalID string) string {
	namespace := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	return uuid.NewSHA1(namespace, []byte(externalID)).String()
}

// ShortHash 返回输入的 16 字符 SHA-256 截断哈希。
func ShortHash(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:8])
}

// MediumHash 返回输入的 32 字符 SHA-256 截断哈希。
func MediumHash(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:16])
}

// SecureHash 返回输入的完整 SHA-256 十六进制哈希。
func SecureHash(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:])
}

// HashCodeHex 是 SecureHash 的兼容别名。
func HashCodeHex(s string) string {
	return SecureHash(s)
}

// HashCode64 返回输入 SHA-256 摘要前 8 字节对应的无符号整数。
func HashCode64(s string) uint64 {
	hash := sha256.Sum256([]byte(s))
	return binary.BigEndian.Uint64(hash[:8])
}

// HashCodes 返回多个字符串按旧分隔格式组合后的 32 字符哈希。
func HashCodes(strings ...string) string {
	var buf bytes.Buffer
	for _, s := range strings {
		buf.WriteString(fmt.Sprintf("%s-", s))
	}
	return MediumHash(buf.String())
}
