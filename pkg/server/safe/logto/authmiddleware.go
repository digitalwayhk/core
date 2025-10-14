package logto

import (
	"fmt"
	"log"
	"net/http"
	"strings"

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
			http.Error(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
			return
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
			http.Error(w, "Invalid audience", http.StatusUnauthorized)
			return
		}

		// 校验 iss
		if iss, ok := claims["iss"].(string); !ok || iss != issuer {
			http.Error(w, "Invalid issuer", http.StatusUnauthorized)
			return
		}

		// ✅ 验证通过
		next.ServeHTTP(w, r)
	})
}

func AuthHandler(next http.HandlerFunc, _issuer, _expectedAudience string) http.Handler {
	if _issuer != "" {
		issuer = _issuer
	}
	if _expectedAudience != "" {
		expectedAudience = _expectedAudience
	}
	// 加载 Logto 服务器的 JWKS
	jwks, err := keyfunc.Get(jwksURL, keyfunc.Options{})
	if err != nil {
		log.Fatalf("Failed to get JWKS from %s: %v", jwksURL, err)
	}
	return AuthMiddleware(jwks, next)

}

func GetUserHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, authorized user!")
}

func main() {
	// 加载 Logto JWKS
	jwks, err := keyfunc.Get(jwksURL, keyfunc.Options{})
	if err != nil {
		log.Fatalf("Failed to get JWKS from %s: %v", jwksURL, err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/getuser", AuthMiddleware(jwks, http.HandlerFunc(GetUserHandler)))

	fmt.Println("✅ API server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
