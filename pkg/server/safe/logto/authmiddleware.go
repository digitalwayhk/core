package logto

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/golang-jwt/jwt/v5"
)

var (
	// 从 Logto 控制台 -> API 资源 -> Identifier=appid
	expectedAudience = "<appid>"

	// Logto 租户域名 (替换成你自己的域名，比如 https://your-tenant.logto.app)
	issuer = "https://srph37.logto.app"

	// JWKS URL
	jwksURL = issuer + "/oidc/jwks"
)

// AuthMiddleware 验证 JWT
func AuthMiddleware(jwks *keyfunc.JWKS, next http.Handler) http.Handler {
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
		token, err := jwt.Parse(tokenString, jwks.Keyfunc)
		if err != nil {
			// ✅ 如果 key 未找到，尝试刷新 JWKS
			if strings.Contains(err.Error(), "the given key ID was not found in the JWKS") {
				log.Printf("⚠️ Key ID not found, refreshing JWKS...")

				// 强制刷新 JWKS
				if refreshErr := jwks.Refresh(r.Context(), keyfunc.RefreshOptions{}); refreshErr != nil {
					log.Printf("❌ Failed to refresh JWKS: %v", refreshErr)
					http.Error(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
					return
				}

				// 重新验证 token
				token, err = jwt.Parse(tokenString, jwks.Keyfunc)
				if err != nil {
					log.Printf("❌ Token validation failed after JWKS refresh: %v", err)
					http.Error(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
					return
				}
			} else {
				log.Printf("❌ Token validation error: %v", err)
				http.Error(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
				return
			}
		}

		if !token.Valid {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// 校验 claims
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "Invalid claims", http.StatusUnauthorized)
			return
		}

		// 校验 aud
		if aud, ok := claims["aud"].(string); !ok || aud != expectedAudience {
			http.Error(w, fmt.Sprintf("Invalid audience: expected %s, got %v", expectedAudience, claims["aud"]), http.StatusUnauthorized)
			return
		}
		oidc := issuer + "oidc"
		// 校验 iss
		if iss, ok := claims["iss"].(string); !ok || iss != oidc {
			http.Error(w, fmt.Sprintf("Invalid issuer: expected %s, got %v", oidc, claims["iss"]), http.StatusUnauthorized)
			return
		}
		// resp, err := http.Get(oidc + "/me")
		// if err != nil {
		// 	http.Error(w, "Failed to get user info: "+err.Error(), http.StatusUnauthorized)
		// 	return
		// }
		// defer resp.Body.Close()

		// if resp.StatusCode != http.StatusOK {
		// 	http.Error(w, "Failed to get user info: "+resp.Status, http.StatusUnauthorized)
		// 	return
		// }

		// ✅ 验证通过
		//log.Printf("✅ Token validated for user: %v", claims["sub"])
		next.ServeHTTP(w, r)
	})
}

// AuthHandler 创建带自动刷新的认证处理器
func AuthHandler(next http.HandlerFunc, _issuer, _expectedAudience string) http.Handler {
	if _issuer != "" {
		issuer = _issuer
		jwksURL = issuer + "/oidc/jwks"
	}
	if _expectedAudience != "" {
		expectedAudience = _expectedAudience
	}

	// ✅ 配置 JWKS 自动刷新
	options := keyfunc.Options{
		// 每 1 小时刷新一次 JWKS
		RefreshInterval: 1 * time.Hour,

		// 刷新错误时的处理
		RefreshErrorHandler: func(err error) {
			log.Printf("⚠️ JWKS refresh error: %v", err)
		},

		// 请求超时
		RefreshTimeout: 10 * time.Second,

		// 刷新未知的 key ID
		RefreshUnknownKID: true,
	}

	// 加载 Logto 服务器的 JWKS
	jwks, err := keyfunc.Get(jwksURL, options)
	if err != nil {
		log.Fatalf("❌ Failed to get JWKS from %s: %v", jwksURL, err)
	}

	log.Printf("✅ JWKS loaded successfully from %s", jwksURL)
	return AuthMiddleware(jwks, next)
}

func GetUserHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, authorized user!")
}

// func main() {
// 	// ✅ 配置 JWKS 自动刷新
// 	options := keyfunc.Options{
// 		RefreshInterval:   1 * time.Hour,
// 		RefreshTimeout:    10 * time.Second,
// 		RefreshUnknownKID: true,
// 		RefreshErrorHandler: func(err error) {
// 			log.Printf("⚠️ JWKS refresh error: %v", err)
// 		},
// 	}

// 	// 加载 Logto JWKS
// 	jwks, err := keyfunc.Get(jwksURL, options)
// 	if err != nil {
// 		log.Fatalf("❌ Failed to get JWKS from %s: %v", jwksURL, err)
// 	}

// 	mux := http.NewServeMux()
// 	mux.Handle("/api/getuser", AuthMiddleware(jwks, http.HandlerFunc(GetUserHandler)))

// 	fmt.Println("✅ API server running on http://localhost:8080")
// 	log.Fatal(http.ListenAndServe(":8080", mux))
// }
