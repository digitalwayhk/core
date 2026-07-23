package casdoor

import (
	"errors"
	"net/http"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
)

var ErrClientRequired = errors.New("explicit Casdoor client is required")

// AuthMiddleware 仅为保持旧函数签名而保留。
// Deprecated: 旧签名无法表达 Auth/Manage Client，固定 fail closed；
// 请使用 ServiceContext 注册的 REST 认证链。
func AuthMiddleware(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "authentication failed", http.StatusUnauthorized)
}

// AuthHandler 仅为保持旧函数签名而保留。
// Deprecated: 旧签名无法表达 Auth/Manage Client，固定 fail closed；
// 请使用 ServiceContext 注册的 REST 认证链。
func AuthHandler(_ http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "authentication failed", http.StatusUnauthorized)
	})
}

// TokenParse 仅为保持旧函数签名而保留。
// Deprecated: 使用 TokenParseWithClient，并显式传入认证域 Client。
func TokenParse(string) (*casdoorsdk.Claims, error) {
	return nil, ErrClientRequired
}

// TokenParseWithClient 仅解析原始 Casdoor JWT，不包含框架 Access Token
// 的用途隔离、撤销世代和业务授权校验，不得将解析成功作为授权结论。
func TokenParseWithClient(client *DomainClient, token string) (*casdoorsdk.Claims, error) {
	if client == nil {
		return nil, ErrClientRequired
	}
	return client.ParseJwtToken(token)
}

// NewAuthHandler 仅为保持旧函数签名而保留。
// Deprecated: 该签名无法提供框架 Access Secret、认证域和撤销权威，
// 因此固定 fail closed。请使用 ServiceContext 注册的 REST 认证链。
func NewAuthHandler(_ *DomainClient, _ http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "authentication failed", http.StatusUnauthorized)
	})
}
