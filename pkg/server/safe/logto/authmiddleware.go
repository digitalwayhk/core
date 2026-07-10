package logto

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/zeromicro/go-zero/core/logx"
)

type AuthConfig struct {
	Issuer           string
	ExpectedAudience string
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
			http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
			return
		}

		// 使用 JWKS 验证
		parseToken := func() (*jwt.Token, error) {
			return jwt.Parse(tokenString, jwks.Keyfunc,
				jwt.WithAudience(cfg.ExpectedAudience),
				jwt.WithIssuer(cfg.issuerClaim()),
			)
		}
		token, err := parseToken()
		if err != nil {
			if strings.Contains(err.Error(), "the given key ID was not found in the JWKS") {
				if refreshErr := jwks.Refresh(r.Context(), keyfunc.RefreshOptions{}); refreshErr != nil {
					http.Error(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
					return
				}

				token, err = parseToken()
				if err != nil {
					http.Error(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
					return
				}
			} else {
				http.Error(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
				return
			}
		}

		if !token.Valid {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func NewAuthHandler(next http.HandlerFunc, cfg AuthConfig) (http.Handler, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	options := keyfunc.Options{
		RefreshInterval: 1 * time.Hour,
		RefreshErrorHandler: func(err error) {
			logx.Errorw("jwks_refresh_failed", logx.Field("error", err))
		},
		RefreshTimeout:    10 * time.Second,
		RefreshUnknownKID: true,
	}

	jwks, err := keyfunc.Get(cfg.jwksURL(), options)
	if err != nil {
		return nil, fmt.Errorf("load Logto JWKS: %w", err)
	}
	return AuthMiddleware(jwks, next, cfg), nil
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
