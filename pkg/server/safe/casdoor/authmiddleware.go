package casdoor

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
)

var ErrClientRequired = errors.New("explicit Casdoor client is required")

// AuthMiddleware 仅为保持旧函数签名而保留。
// Deprecated: 旧签名无法表达 Auth/Manage Client，固定 fail closed；使用 NewAuthHandler。
func AuthMiddleware(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "authentication failed", http.StatusUnauthorized)
}

// AuthHandler 仅为保持旧函数签名而保留。
// Deprecated: 旧签名无法表达 Auth/Manage Client，固定 fail closed；使用 NewAuthHandler。
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

func TokenParseWithClient(client *DomainClient, token string) (*casdoorsdk.Claims, error) {
	if client == nil {
		return nil, ErrClientRequired
	}
	return client.ParseJwtToken(token)
}

// NewAuthHandler 使用显式 Casdoor 域 Client 验证 Token，避免 SDK 全局状态串域。
func NewAuthHandler(client *DomainClient, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}
		claims, err := TokenParseWithClient(client, token)
		if err != nil || claims == nil {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}
		userJSON, err := json.Marshal(claims.User)
		if err != nil {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}
		r.Header.Set("Casdoor-User-Json", string(userJSON))
		next.ServeHTTP(w, r)
	})
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return token, token != ""
}
