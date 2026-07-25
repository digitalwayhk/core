package safe

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/pbkdf2"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

type Claims struct {
	Uid         string            `json:"userid"`
	Uname       string            `json:"username"`
	Args        map[string]string `json:"args"`
	EncryptKey  string            `json:"-"`
	secretArgs  map[string]string
	secretKey   []byte
	secretType  types.AuthType
	secretErr   error
	secretSizes map[string]int
}

func NewClaims(userId string, username string) *Claims {
	return &Claims{
		Uid:   userId,
		Uname: username,
		Args:  make(map[string]string),
		// EncryptKey 仅保留旧对象内加解密兼容，不用于 Token 秘密 Claim。
		EncryptKey: uuid.NewString(),
	}
}
func (own *Claims) SetEncryptKey(key string) *Claims {
	own.EncryptKey = key
	return own
}
func (own *Claims) AddData(key string, value string) {
	if own.Args == nil {
		own.Args = make(map[string]string)
	}
	own.Args[key] = value
}
func (own *Claims) GetData(key string) (string, error) {
	if own.Args == nil {
		return "", errors.New("无数据")
	}
	if value, exists := own.Args[key]; exists {
		return value, nil
	}
	return "", errors.New("未找到数据")
}

// GetToken 保留用于源代码兼容；新代码应使用 IssueTokenPair 生成带用途隔离的 Token。
// Deprecated: 使用 IssueTokenPair。
func (own *Claims) GetToken(secret string, expire int64) (string, error) {
	if own == nil {
		return "", errors.New("Claims 不能为空")
	}
	if own.secretErr != nil {
		return "", fmt.Errorf("秘密 Claim 无效: %w", own.secretErr)
	}
	iat := time.Now().Unix()
	claims := make(jwt.MapClaims)
	claims["exp"] = iat + expire
	claims["iat"] = iat
	claims["uid"] = own.Uid // 🔧 转为字符串存储
	claims["uname"] = own.Uname
	if own.Args != nil {
		for k, v := range own.Args {
			claims[k] = v
		}
	}
	if len(own.secretArgs) > 0 {
		if err := own.validateSecretContext(secret, own.secretType); err != nil {
			return "", fmt.Errorf("秘密 Claim 无效: %w", err)
		}
		claims[secretArgsClaim] = cloneStringMap(own.secretArgs)
	}
	token := jwt.New(jwt.SigningMethodHS256)
	token.Claims = claims
	return token.SignedString([]byte(secret))
}

// ValidateJWTToken 保留为最终用户 Access Token 的兼容包装。
// Deprecated: 使用 ValidateAccessToken 并显式传入期望的认证类型和当前时间。
func ValidateJWTToken(tokenString string, auth config.AuthSecret) (string, string, error) {
	identity, err := ValidateAccessToken(tokenString, auth.AccessSecret, types.AuthTypeUser, time.Now())
	if err != nil {
		return "", "", err
	}
	return identity.UID, identity.Username, nil
}

func GetJWTExpiry(tokenString string) int64 {
	token, _ := jwt.Parse(tokenString, nil)
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		if exp, exists := claims["exp"]; exists {
			if expFloat, ok := exp.(float64); ok {
				return int64(expFloat)
			}
		}
	}
	return 0
}

// 🔧 改进：更安全的JWT密钥派生
func DeriveJWTKey(password, userID string) string {
	// 使用用户ID作为盐值的一部分，确保每个用户的密钥不同
	salt := fmt.Sprintf("jwt-salt-%s-v1", userID)                             // 添加版本号方便升级
	key := pbkdf2.Key([]byte(password), []byte(salt), 100000, 32, sha256.New) // 增加迭代次数
	return base64.URLEncoding.EncodeToString(key)
}

// 🔧 新增：支持自定义盐值的JWT密钥派生
func DeriveJWTKeyWithCustomSalt(password, userID, customSalt string) string {
	salt := fmt.Sprintf("%s-jwt-%s", customSalt, userID)
	key := pbkdf2.Key([]byte(password), []byte(salt), 100000, 32, sha256.New)
	return base64.URLEncoding.EncodeToString(key)
}

// 🔧 新增：为不同用途派生不同的密钥
func DeriveKeyForPurpose(password, userID, purpose string) string {
	salt := fmt.Sprintf("%s-%s-v1", purpose, userID)
	key := pbkdf2.Key([]byte(password), []byte(salt), 100000, 32, sha256.New)
	return base64.URLEncoding.EncodeToString(key)
}
