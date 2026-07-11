package logto

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/zeromicro/go-zero/core/logx"
)

type AuthConfig struct {
	Issuer           string
	ExpectedAudience string
}

type HandlerFactory struct {
	mu     sync.Mutex
	jwks   map[AuthConfig]*keyfunc.JWKS
	closed bool
}

func NewHandlerFactory() *HandlerFactory {
	return &HandlerFactory{jwks: make(map[AuthConfig]*keyfunc.JWKS)}
}

func (c AuthConfig) validate() error {
	if strings.TrimSpace(c.Issuer) == "" {
		return errors.New("Logto issuer is required")
	}
	if strings.TrimSpace(c.ExpectedAudience) == "" {
		return errors.New("Logto expected audience is required")
	}
	return nil
}

func (c AuthConfig) issuerClaim() string {
	issuer := strings.TrimRight(strings.TrimSpace(c.Issuer), "/")
	if strings.HasSuffix(issuer, "/oidc") {
		return issuer
	}
	return issuer + "/oidc"
}

func (c AuthConfig) jwksURL() string {
	return c.issuerClaim() + "/jwks"
}

// AuthMiddleware 验证 JWT
func AuthMiddleware(jwks *keyfunc.JWKS, next http.Handler, cfg AuthConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeAuthenticationFailed(w)
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			writeAuthenticationFailed(w)
			return
		}

		token, err := jwt.Parse(tokenString, jwks.Keyfunc,
			jwt.WithAudience(cfg.ExpectedAudience),
			jwt.WithIssuer(cfg.issuerClaim()),
		)
		if err != nil {
			writeAuthenticationFailed(w)
			return
		}

		if !token.Valid {
			writeAuthenticationFailed(w)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			writeAuthenticationFailed(w)
			return
		}
		uid := firstStringClaim(claims, "uid", "sub")
		if uid == "" {
			writeAuthenticationFailed(w)
			return
		}
		ctx := context.WithValue(r.Context(), "uid", uid)
		if uname := firstStringClaim(claims, "uname", "username", "name"); uname != "" {
			ctx = context.WithValue(ctx, "uname", uname)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func firstStringClaim(claims jwt.MapClaims, keys ...string) string {
	for _, key := range keys {
		if value, ok := claims[key].(string); ok {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	return ""
}

func writeAuthenticationFailed(w http.ResponseWriter) {
	http.Error(w, "authentication failed", http.StatusUnauthorized)
}

func NewAuthHandler(next http.HandlerFunc, cfg AuthConfig) (http.Handler, error) {
	jwks, err := newJWKS(cfg)
	if err != nil {
		return nil, err
	}
	return AuthMiddleware(jwks, next, cfg), nil
}

func newJWKS(cfg AuthConfig) (*keyfunc.JWKS, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	options := keyfunc.Options{
		RefreshInterval: 1 * time.Hour,
		RefreshErrorHandler: func(err error) {
			logx.Errorw("jwks_refresh_failed", logx.Field("error", err))
		},
		RefreshTimeout:    10 * time.Second,
		RefreshRateLimit:  5 * time.Minute,
		RefreshUnknownKID: true,
	}

	jwks, err := keyfunc.Get(cfg.jwksURL(), options)
	if err != nil {
		return nil, fmt.Errorf("load Logto JWKS: %w", err)
	}
	return jwks, nil
}

func (f *HandlerFactory) NewAuthHandler(next http.HandlerFunc, cfg AuthConfig) (http.Handler, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, errors.New("Logto handler factory is closed")
	}
	jwks := f.jwks[cfg]
	if jwks == nil {
		var err error
		jwks, err = newJWKS(cfg)
		if err != nil {
			return nil, err
		}
		f.jwks[cfg] = jwks
	}
	return AuthMiddleware(jwks, next, cfg), nil
}

func (f *HandlerFactory) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	for _, jwks := range f.jwks {
		jwks.EndBackground()
	}
	f.jwks = nil
	f.closed = true
}

// AuthHandler is kept for source compatibility. New server code must use
// NewAuthHandler so JWKS initialization errors stop startup explicitly.
func AuthHandler(next http.HandlerFunc, issuer, expectedAudience string) http.Handler {
	handler, err := NewAuthHandler(next, AuthConfig{
		Issuer:           issuer,
		ExpectedAudience: expectedAudience,
	})
	if err == nil {
		return handler
	}
	logx.Errorw("jwks_initialization_failed", logx.Field("error", err))
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
	})
}

func GetUserHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, authorized user!")
}
