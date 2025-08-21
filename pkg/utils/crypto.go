package utils

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

// 填充字符串（末尾）
func PaddingText(str []byte, blockSize int) []byte {
	//需要填充的数据长度
	paddingCount := blockSize - len(str)%blockSize
	//填充数据为：paddingCount ,填充的值为：paddingCount
	paddingStr := bytes.Repeat([]byte{byte(paddingCount)}, paddingCount)
	newPaddingStr := append(str, paddingStr...)
	//fmt.Println(newPaddingStr)
	return newPaddingStr
}

// 去掉字符（末尾）
func UnPaddingText(str []byte) []byte {
	n := len(str)
	count := int(str[n-1])
	newPaddingText := str[:n-count]
	return newPaddingText
}

// ---------------DES加密  解密--------------------
func EncyptogDES(src, key []byte) []byte {
	//1、创建并返回一个使用DES算法的cipher.Block接口
	block, _ := des.NewCipher(key)
	//2、对数据进行填充
	src1 := PaddingText(src, block.BlockSize())

	//3.创建一个密码分组为链接模式，底层使用des加密的blockmode接口
	iv := []byte("aaaabbbb")
	blockMode := cipher.NewCBCEncrypter(block, iv)
	//4加密连续的数据块
	desc := make([]byte, len(src1))
	blockMode.CryptBlocks(desc, src1)
	return desc
}
func DecrptogDES(src, key []byte) []byte {
	//创建一个block的接口
	block, _ := des.NewCipher(key)
	iv := []byte("aaaabbbb")
	//链接模式，创建blockMode接口
	blockeMode := cipher.NewCBCDecrypter(block, iv)
	//解密
	blockeMode.CryptBlocks(src, src)
	//去掉填充
	newText := UnPaddingText(src)
	return newText
}

// ---------------DES加密  解密--------------------
func Encyptog3DES(src, key []byte) []byte {
	//des包下的三次加密接口
	block, _ := des.NewTripleDESCipher(key)
	src = PaddingText(src, block.BlockSize())
	blockMode := cipher.NewCBCEncrypter(block, key[:block.BlockSize()])
	blockMode.CryptBlocks(src, src)
	return src
}
func Decrptog3DES(src, key []byte) []byte {
	block, _ := des.NewTripleDESCipher(key)
	blockMode := cipher.NewCBCDecrypter(block, key[:block.BlockSize()])
	blockMode.CryptBlocks(src, src)
	src = UnPaddingText(src)
	return src
}

// ---------------AES加密  解密--------------------
func EncryptAES(plaintext, key string) (string, error) {
	// 使用SHA256生成32字节密钥
	hash := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(hash[:])
	if err != nil {
		return "", err
	}

	// 使用GCM模式
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// 生成随机nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// 加密
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Base64编码
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

func DecryptAES(ciphertext, key string) (string, error) {
	// Base64解码
	data, err := base64.URLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	// 使用SHA256生成32字节密钥
	hash := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(hash[:])
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext_bytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext_bytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// 🔧 安全的密钥派生
func DeriveKey(password, salt string, keyLen int) []byte {
	return pbkdf2.Key([]byte(password), []byte(salt), 10000, keyLen, sha256.New)
}

// 🔧 改进：生成随机盐值
func GenerateSalt(length int) ([]byte, error) {
	salt := make([]byte, length)
	_, err := rand.Read(salt)
	return salt, err
}

// 🔧 改进：更安全的密钥派生
func DeriveKeySecure(password string, salt []byte, keyLen int) []byte {
	// 现代推荐的迭代次数：100,000 - 600,000
	iterations := 100000
	return pbkdf2.Key([]byte(password), salt, iterations, keyLen, sha256.New)
}

// 🔧 便利函数：自动生成盐值
func DeriveKeyWithSalt(password string, keyLen int) ([]byte, []byte, error) {
	// 生成16字节随机盐值
	salt, err := GenerateSalt(16)
	if err != nil {
		return nil, nil, err
	}

	key := DeriveKeySecure(password, salt, keyLen)
	return key, salt, nil
}

// 🔧 用于JWT的密钥派生
func DeriveJWTKey(password, userID string) string {
	// 使用用户ID作为盐值的一部分，确保每个用户的密钥不同
	salt := fmt.Sprintf("jwt-salt-%s", userID)
	key := pbkdf2.Key([]byte(password), []byte(salt), 50000, 32, sha256.New)
	return base64.URLEncoding.EncodeToString(key)
}
