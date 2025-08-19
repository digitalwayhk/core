package safe

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

type Claims struct {
	Uid   uint                   `json:"userid"`
	Uname string                 `json:"username"`
	Args  map[string]interface{} `json:"args"`
}

func NewClaims(userId uint, username string) *Claims {
	return &Claims{
		Uid:   userId,
		Uname: username,
	}
}

func GetToken(uid uint, secret string, expire int64) (string, error) {
	iat := time.Now().Unix()
	claims := make(jwt.MapClaims)
	claims["exp"] = iat + expire
	claims["iat"] = iat
	claims["uid"] = uid
	token := jwt.New(jwt.SigningMethodHS256)
	token.Claims = claims
	return token.SignedString([]byte(secret))
}

func (own *Claims) GetToken(secret string, expire int64) (string, error) {
	iat := time.Now().Unix()
	claims := make(jwt.MapClaims)
	claims["exp"] = iat + expire
	claims["iat"] = iat
	claims["uid"] = own.Uid
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

func ValidateJWTToken(tokenString, secret string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})

	if err != nil {
		return "", err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if userID, exists := claims["uid"]; exists {
			return fmt.Sprintf("%v", userID), nil
		}
		// if sub, exists := claims["sub"]; exists {
		// 	return fmt.Sprintf("%v", sub), nil
		// }
		return "", errors.New("token中未找到用户ID")
	}

	return "", errors.New("invalid token")
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
