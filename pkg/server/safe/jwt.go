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
	"github.com/digitalwayhk/core/pkg/server/safe/casdoor"
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
	if auth.CasDoor.Enable {
		user, err := casdoor.TokenParse(tokenString)
		if err != nil {
			return "", "", err
		}
		return user.Id, user.Email, nil
	}
	return defaultToken(tokenString, auth.AccessSecret)
}

func defaultToken(tokenString, secret string) (string, string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})

	if err != nil {
		return "", "", err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		uidStr, unameStr := "", ""
		if uname, ok := claims["uname"].(string); ok {
			unameStr = uname
		}
		if uid, ok := claims["uid"].(string); ok {
			uidStr = uid
		}
		if uidStr != "" {
			return uidStr, unameStr, nil
		}
		return "", "", errors.New("userid is empty")
	}
	return "", "", errors.New("invalid token")
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
