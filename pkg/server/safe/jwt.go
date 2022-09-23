package safe

import (
	"github.com/golang-jwt/jwt/v4"
	"time"
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
