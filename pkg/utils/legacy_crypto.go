// 本文件仅为兼容保留旧 DES、3DES 和 padding API，新代码不应继续使用。
package utils

import (
	"bytes"
	"crypto/cipher"
	"crypto/des"
)

// PaddingText 按 PKCS#7 风格填充数据。
func PaddingText(str []byte, blockSize int) []byte {
	paddingCount := blockSize - len(str)%blockSize
	paddingStr := bytes.Repeat([]byte{byte(paddingCount)}, paddingCount)
	return append(str, paddingStr...)
}

// UnPaddingText 按旧语义移除末尾 padding。
func UnPaddingText(str []byte) []byte {
	n := len(str)
	count := int(str[n-1])
	return str[:n-count]
}

// EncyptogDES 使用旧固定 IV 的 DES-CBC 算法加密数据。
func EncyptogDES(src, key []byte) []byte {
	block, _ := des.NewCipher(key)
	src1 := PaddingText(src, block.BlockSize())
	iv := []byte("aaaabbbb")
	blockMode := cipher.NewCBCEncrypter(block, iv)
	desc := make([]byte, len(src1))
	blockMode.CryptBlocks(desc, src1)
	return desc
}

// DecrptogDES 使用旧固定 IV 的 DES-CBC 算法解密数据。
func DecrptogDES(src, key []byte) []byte {
	block, _ := des.NewCipher(key)
	iv := []byte("aaaabbbb")
	blockMode := cipher.NewCBCDecrypter(block, iv)
	blockMode.CryptBlocks(src, src)
	return UnPaddingText(src)
}

// Encyptog3DES 使用旧 key 派生 IV 的 3DES-CBC 算法加密数据。
func Encyptog3DES(src, key []byte) []byte {
	block, _ := des.NewTripleDESCipher(key)
	src = PaddingText(src, block.BlockSize())
	blockMode := cipher.NewCBCEncrypter(block, key[:block.BlockSize()])
	blockMode.CryptBlocks(src, src)
	return src
}

// Decrptog3DES 使用旧 key 派生 IV 的 3DES-CBC 算法解密数据。
func Decrptog3DES(src, key []byte) []byte {
	block, _ := des.NewTripleDESCipher(key)
	blockMode := cipher.NewCBCDecrypter(block, key[:block.BlockSize()])
	blockMode.CryptBlocks(src, src)
	return UnPaddingText(src)
}
