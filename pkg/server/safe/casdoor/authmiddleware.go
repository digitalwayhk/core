package casdoor

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
)

func AuthMiddleware(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "authHeader is empty", http.StatusUnauthorized)
		return
	}

	token := strings.Split(authHeader, "Bearer ")
	if len(token) != 2 {
		http.Error(w, "token is not valid Bearer token", http.StatusUnauthorized)
		return
	}

	claims, err := casdoorsdk.ParseJwtToken(token[1])
	if err != nil {
		http.Error(w, "ParseJwtToken() error", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"data":   claims.User,
	})
}

func AuthHandler(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "authHeader is empty", http.StatusUnauthorized)
			return
		}

		token := strings.Split(authHeader, "Bearer ")
		if len(token) != 2 {
			http.Error(w, "token is not valid Bearer token", http.StatusUnauthorized)
			return
		}

		claims, err := TokenParse(token[1])
		if err != nil {
			http.Error(w, "ParseJwtToken() error: "+err.Error(), http.StatusUnauthorized)
			return
		}
		userJson, _ := json.Marshal(claims.User)
		con := r.Context()
		r = r.WithContext(context.WithValue(con, "uid", claims.User.Id))
		r = r.WithContext(context.WithValue(con, "uname", claims.User.Email))
		r = r.WithContext(context.WithValue(con, "user", claims.User))
		r.Header.Set("Casdoor-User-Json", string(userJson))
		next.ServeHTTP(w, r)
	})
}

func TokenParse(tokenString string) (*casdoorsdk.Claims, error) {
	claims, err := casdoorsdk.ParseJwtToken(tokenString)
	if err != nil {
		return nil, err
	}
	return claims, nil
}
