package safe

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
)

type Claims struct {
	Uid        string            `json:"userid"`
	Uname      string            `json:"username"`
	Args       map[string]string `json:"args"`
	EncryptKey string            `json:"-"`
}

func NewClaims(userId string, username string) *Claims {
	uuid := uuid.New().String()
	return &Claims{
		Uid:   userId,
		Uname: username,
		Args:  make(map[string]string),
		//生成个随机安全值
		EncryptKey: uuid,
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
	nv := value
	if isSensitiveKey(key) {
		var err error
		nv, err = utils.EncryptAES(nv, own.EncryptKey)
		if err != nil {
			logx.Errorf("加密失败: %v", err)
		}
	}
	own.Args[key] = nv
}
func (own *Claims) GetData(key string) (string, error) {
	if own.Args == nil {
		return "", errors.New("无数据")
	}
	if value, exists := own.Args[key]; exists {
		if isSensitiveKey(key) {
			var err error
			value, err = utils.DecryptAES(value, own.EncryptKey)
			if err != nil {
				logx.Errorf("解密失败: %v", err)
				return "", err
			}
		}
		return value, nil
	}
	return "", errors.New("未找到数据")
}

// GetToken 保留用于源代码兼容；新代码应使用 IssueTokenPair 生成带用途隔离的 Token。
// Deprecated: 使用 IssueTokenPair。
func (own *Claims) GetToken(secret string, expire int64) (string, error) {
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
	token := jwt.New(jwt.SigningMethodHS256)
	token.Claims = claims
	return token.SignedString([]byte(secret))
}

// 判断是否为敏感字段
func isSensitiveKey(key string) bool {
	sensitiveKeys := []string{"email", "phone", "real", "card", "id", "name"}
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(key, sensitive) {
			return true
		}
	}
	return false
}
func ValidateJWTToken(tokenString string, auth config.AuthSecret) (string, string, error) {
	return defaultToken(tokenString, auth.AccessSecret)
}

func defaultToken(tokenString, secret string) (string, string, error) {
	if tokenString == "" || secret == "" {
		return "", "", errors.New("Access Token 验证参数无效")
	}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithoutClaimsValidation(),
	)
	token, err := parser.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("Access Token 签名算法无效")
		}
		return []byte(secret), nil
	})
	if err != nil || token == nil || !token.Valid {
		return "", "", errors.New("Access Token 签名无效")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", errors.New("Access Token Claims 无效")
	}

	uid, _ := claims["uid"].(string)
	username, _ := claims["uname"].(string)
	authType, _ := claims["auth_type"].(string)
	tokenUse, _ := claims["token_use"].(string)
	issuedAt, iatOK := numericDate(claims["iat"])
	expiresAt, expOK := numericDate(claims["exp"])
	now := time.Now().Unix()
	if uid == "" || tokenUse != "access" || authType != "auth" || !iatOK || !expOK {
		return "", "", errors.New("Access Token Claims 不完整")
	}
	if expiresAt <= now || issuedAt > now || issuedAt > expiresAt {
		return "", "", errors.New("Access Token 已过期或时间无效")
	}
	return uid, username, nil
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
