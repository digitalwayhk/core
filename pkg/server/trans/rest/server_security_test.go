package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/require"
)

func TestRestRunOptionsDisabledCors(t *testing.T) {
	opts, err := restRunOptions(false, nil)

	require.NoError(t, err)
	require.Empty(t, opts)
}

func TestRestRunOptionsRejectsMissingOrigins(t *testing.T) {
	for _, origins := range [][]string{nil, {}, {"", "  "}} {
		_, err := restRunOptions(true, origins)
		require.ErrorContains(t, err, "CORS origin")
	}
}

func TestNormalizeCorsOriginsPreservesExplicitOrigins(t *testing.T) {
	origins := normalizeCorsOrigins([]string{" https://admin.example.com ", "", "*"})

	require.Equal(t, []string{"https://admin.example.com", "*"}, origins)
}

func TestNewLogtoHandlerRejectsInvalidConfig(t *testing.T) {
	_, err := newLogtoHandler(func(http.ResponseWriter, *http.Request) {}, config.LogtoConfig{})

	require.ErrorContains(t, err, "issuer")
}

func TestSecurityHeaders(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "no-referrer", recorder.Header().Get("Referrer-Policy"))
	require.Equal(t, "DENY", recorder.Header().Get("X-Frame-Options"))
}

func TestSecurityHeadersPreserveExistingValues(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Referrer-Policy", "same-origin")

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, "same-origin", recorder.Header().Get("Referrer-Policy"))
}
