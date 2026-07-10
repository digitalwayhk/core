package logto

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const testKeyID = "auth-test-key"

func TestAuthHandlersKeepIndependentPolicy(t *testing.T) {
	secret := []byte("auth-test-secret-with-enough-entropy")
	jwks := keyfunc.NewGiven(map[string]keyfunc.GivenKey{
		testKeyID: keyfunc.NewGivenHMAC(secret, keyfunc.GivenKeyOptions{Algorithm: jwt.SigningMethodHS256.Alg()}),
	})

	handlerA := AuthMiddleware(jwks, successHandler(), AuthConfig{
		Issuer:           "https://tenant-a.example",
		ExpectedAudience: "api-a",
	})
	handlerB := AuthMiddleware(jwks, successHandler(), AuthConfig{
		Issuer:           "https://tenant-b.example/",
		ExpectedAudience: "api-b",
	})
	tokenA := signToken(t, secret, "https://tenant-a.example/oidc", "api-a")
	tokenB := signToken(t, secret, "https://tenant-b.example/oidc", "api-b")

	type authCase struct {
		handler http.Handler
		token   string
		want    int
	}
	cases := []authCase{
		{handler: handlerA, token: tokenA, want: http.StatusNoContent},
		{handler: handlerB, token: tokenB, want: http.StatusNoContent},
		{handler: handlerA, token: tokenB, want: http.StatusUnauthorized},
		{handler: handlerB, token: tokenA, want: http.StatusUnauthorized},
	}

	var wg sync.WaitGroup
	results := make(chan [2]int, len(cases)*20)
	for range 20 {
		for _, tc := range cases {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results <- [2]int{requestStatus(tc.handler, tc.token), tc.want}
			}()
		}
	}
	wg.Wait()
	close(results)

	for result := range results {
		require.Equal(t, result[1], result[0])
	}
}

func TestNewAuthHandlerRejectsInvalidConfig(t *testing.T) {
	_, err := NewAuthHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), AuthConfig{})

	require.ErrorContains(t, err, "issuer")
}

func successHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func signToken(t *testing.T, secret []byte, issuer, audience string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": issuer,
		"aud": audience,
	})
	token.Header["kid"] = testKeyID
	signed, err := token.SignedString(secret)
	require.NoError(t, err)
	return signed
}

func requestStatus(handler http.Handler, token string) int {
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder.Code
}
