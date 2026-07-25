package logto

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const testKeyID = "auth-test-key"

func TestAuthHandlersKeepIndependentPolicy(t *testing.T) {
	secret := []byte("auth-test-secret-with-enough-entropy")
	jwks := testJWKS(secret)

	handlerA := AuthMiddleware(jwks, successHandler(), AuthConfig{
		Issuer:           "https://tenant-a.example",
		ExpectedAudience: "api-a",
	})
	handlerB := AuthMiddleware(jwks, successHandler(), AuthConfig{
		Issuer:           "https://tenant-b.example/",
		ExpectedAudience: "api-b",
	})
	tokenA := signIdentityToken(t, secret, "https://tenant-a.example/oidc", "api-a")
	tokenB := signIdentityToken(t, secret, "https://tenant-b.example/oidc", "api-b")

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

func TestHandlerFactoryReusesJWKSAndRejectsUseAfterClose(t *testing.T) {
	var requests atomic.Int32
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/oidc/jwks" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	t.Cleanup(issuer.Close)

	factory := NewHandlerFactory()
	cfg := AuthConfig{Issuer: issuer.URL, ExpectedAudience: "api"}
	for range 2 {
		_, err := factory.NewAuthHandler(func(http.ResponseWriter, *http.Request) {}, cfg)
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), requests.Load())

	factory.Close()
	_, err := factory.NewAuthHandler(func(http.ResponseWriter, *http.Request) {}, cfg)
	require.ErrorContains(t, err, "closed")
}

func TestAuthResponseDoesNotDiscloseCause(t *testing.T) {
	secret := []byte("auth-test-secret-with-enough-entropy")
	handler := AuthMiddleware(testJWKS(secret), successHandler(), AuthConfig{
		Issuer:           "https://tenant.example",
		ExpectedAudience: "expected-api",
	})
	wrongAudience := signIdentityToken(t, secret, "https://tenant.example/oidc", "private-api-name")

	tests := []struct {
		name          string
		authorization string
	}{
		{name: "missing header"},
		{name: "invalid scheme", authorization: "Basic credentials"},
		{name: "malformed token", authorization: "Bearer secret-token-fixture"},
		{name: "wrong audience", authorization: "Bearer " + wrongAudience},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/private", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			require.Equal(t, "authentication failed\n", recorder.Body.String())
		})
	}
}

func TestAuthMiddlewareInjectsIdentityContext(t *testing.T) {
	secret := []byte("auth-test-secret-with-enough-entropy")
	identity := make(chan [2]string, 1)
	handler := AuthMiddleware(testJWKS(secret), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, _ := r.Context().Value("uid").(string)
		uname, _ := r.Context().Value("uname").(string)
		identity <- [2]string{uid, uname}
		w.WriteHeader(http.StatusNoContent)
	}), AuthConfig{
		Issuer:           "https://tenant.example",
		ExpectedAudience: "expected-api",
	})
	token := signTokenWithClaims(t, secret, jwt.MapClaims{
		"iss":      "https://tenant.example/oidc",
		"aud":      "expected-api",
		"uid":      "explicit-uid",
		"sub":      "subject-fallback",
		"username": "alice",
	})

	require.Equal(t, http.StatusNoContent, requestStatus(handler, token))
	require.Equal(t, [2]string{"explicit-uid", "alice"}, <-identity)
}

func TestAuthMiddlewareRejectsTokenWithoutIdentity(t *testing.T) {
	secret := []byte("auth-test-secret-with-enough-entropy")
	handler := AuthMiddleware(testJWKS(secret), successHandler(), AuthConfig{
		Issuer:           "https://tenant.example",
		ExpectedAudience: "expected-api",
	})
	token := signToken(t, secret, "https://tenant.example/oidc", "expected-api")
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Equal(t, "authentication failed\n", recorder.Body.String())
}

func testJWKS(secret []byte) *keyfunc.JWKS {
	return keyfunc.NewGiven(map[string]keyfunc.GivenKey{
		testKeyID: keyfunc.NewGivenHMAC(secret, keyfunc.GivenKeyOptions{Algorithm: jwt.SigningMethodHS256.Alg()}),
	})
}

func successHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func signToken(t *testing.T, secret []byte, issuer, audience string) string {
	t.Helper()
	return signTokenWithClaims(t, secret, jwt.MapClaims{
		"iss": issuer,
		"aud": audience,
	})
}

func signIdentityToken(t *testing.T, secret []byte, issuer, audience string) string {
	t.Helper()
	return signTokenWithClaims(t, secret, jwt.MapClaims{
		"iss": issuer,
		"aud": audience,
		"sub": "test-user",
	})
}

func signTokenWithClaims(t *testing.T, secret []byte, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
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
