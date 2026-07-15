package safe

import (
	"errors"
	"fmt"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/golang-jwt/jwt/v4"
)

// TokenPairResponse 是 Callback、TestToken 和 Refresh 共用的 Token 响应。
type TokenPairResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token,omitempty"`
	TokenType        string `json:"token_type"`
	AccessExpiresIn  int64  `json:"access_expires_in"`
	RefreshExpiresIn int64  `json:"refresh_expires_in,omitempty"`
}

// TokenIssueRequest 包含签名 Token 所需的全部已验证输入。
type TokenIssueRequest struct {
	Claims               *Claims
	AuthType             types.AuthType
	IssuedAt             time.Time
	AccessSecret         string
	AccessExpireSeconds  int64
	RefreshSecret        string
	RefreshExpireSeconds int64
	IssueRefresh         bool
}

// RefreshTokenIdentity 是从已验证 Refresh Token 中提取的不可信任边界后身份。
type RefreshTokenIdentity struct {
	UID       string
	Username  string
	AuthType  types.AuthType
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// IssueTokenPair 使用同一 IssuedAt 颁发 Access Token 和可选的 Refresh Token。
func IssueTokenPair(req TokenIssueRequest) (TokenPairResponse, error) {
	if err := validateTokenIssueRequest(req); err != nil {
		return TokenPairResponse{}, err
	}

	accessClaims := baseTokenClaims(req.Claims.Uid, req.Claims.Uname, req.AuthType, "access", req.IssuedAt, req.AccessExpireSeconds)
	for key, value := range req.Claims.Args {
		accessClaims[key] = value
	}
	accessToken, err := signMapClaims(accessClaims, req.AccessSecret)
	if err != nil {
		return TokenPairResponse{}, fmt.Errorf("签名 Access Token 失败: %w", err)
	}

	response := TokenPairResponse{
		AccessToken:     accessToken,
		TokenType:       "Bearer",
		AccessExpiresIn: req.AccessExpireSeconds,
	}
	if !req.IssueRefresh {
		return response, nil
	}

	refreshClaims := baseTokenClaims(req.Claims.Uid, req.Claims.Uname, req.AuthType, "refresh", req.IssuedAt, req.RefreshExpireSeconds)
	refreshToken, err := signMapClaims(refreshClaims, req.RefreshSecret)
	if err != nil {
		return TokenPairResponse{}, fmt.Errorf("签名 Refresh Token 失败: %w", err)
	}
	response.RefreshToken = refreshToken
	response.RefreshExpiresIn = req.RefreshExpireSeconds
	return response, nil
}

func validateTokenIssueRequest(req TokenIssueRequest) error {
	if req.Claims == nil || req.Claims.Uid == "" {
		return errors.New("颁发 Token 时 UID 不能为空")
	}
	if req.AuthType != types.AuthTypeUser && req.AuthType != types.AuthTypeManage && req.AuthType != types.AuthTypeServerManage {
		return errors.New("颁发 Token 时认证类型无效")
	}
	if req.IssuedAt.IsZero() {
		return errors.New("颁发 Token 时 IssuedAt 不能为空")
	}
	if req.AccessSecret == "" || req.AccessExpireSeconds <= 0 {
		return errors.New("Access Token 密钥或超时无效")
	}
	if !req.IssueRefresh {
		return nil
	}
	if req.AuthType == types.AuthTypeServerManage {
		return errors.New("servermanage 不允许颁发 Refresh Token")
	}
	if req.RefreshSecret == "" || req.RefreshExpireSeconds <= 0 {
		return errors.New("Refresh Token 密钥或超时无效")
	}
	if req.AccessSecret == req.RefreshSecret {
		return errors.New("Access Token 与 Refresh Token 必须使用不同密钥")
	}
	return nil
}

func baseTokenClaims(uid, username string, authType types.AuthType, tokenUse string, issuedAt time.Time, expireSeconds int64) jwt.MapClaims {
	return jwt.MapClaims{
		"uid":       uid,
		"uname":     username,
		"auth_type": string(authType),
		"token_use": tokenUse,
		"iat":       issuedAt.Unix(),
		"exp":       issuedAt.Add(time.Duration(expireSeconds) * time.Second).Unix(),
	}
}

func signMapClaims(claims jwt.MapClaims, secret string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ValidateRefreshToken 验证签名、算法、用途、认证类型和有效期后返回身份。
func ValidateRefreshToken(tokenString, secret string, expectedAuthType types.AuthType, now time.Time) (*RefreshTokenIdentity, error) {
	if tokenString == "" || secret == "" || now.IsZero() {
		return nil, errors.New("Refresh Token 验证参数无效")
	}
	if expectedAuthType != types.AuthTypeUser && expectedAuthType != types.AuthTypeManage {
		return nil, errors.New("Refresh Token 认证类型无效")
	}

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithoutClaimsValidation(),
	)
	token, err := parser.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("Refresh Token 签名算法无效")
		}
		return []byte(secret), nil
	})
	if err != nil || token == nil || !token.Valid {
		return nil, errors.New("Refresh Token 签名无效")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("Refresh Token Claims 无效")
	}

	uid, _ := claims["uid"].(string)
	username, _ := claims["uname"].(string)
	authType, _ := claims["auth_type"].(string)
	tokenUse, _ := claims["token_use"].(string)
	issuedAt, iatOK := numericDate(claims["iat"])
	expiresAt, expOK := numericDate(claims["exp"])
	if uid == "" || tokenUse != "refresh" || authType != string(expectedAuthType) || !iatOK || !expOK {
		return nil, errors.New("Refresh Token Claims 不完整")
	}
	if expiresAt <= now.Unix() || issuedAt > now.Unix() || issuedAt > expiresAt {
		return nil, errors.New("Refresh Token 已过期或时间无效")
	}

	return &RefreshTokenIdentity{
		UID:       uid,
		Username:  username,
		AuthType:  expectedAuthType,
		IssuedAt:  time.Unix(issuedAt, 0).UTC(),
		ExpiresAt: time.Unix(expiresAt, 0).UTC(),
	}, nil
}

func numericDate(value interface{}) (int64, bool) {
	switch number := value.(type) {
	case float64:
		return int64(number), number == float64(int64(number))
	case int64:
		return number, true
	case int:
		return int64(number), true
	case jwt.NumericDate:
		return number.Unix(), true
	default:
		return 0, false
	}
}
