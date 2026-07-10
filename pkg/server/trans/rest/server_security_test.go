package rest

import (
	"net/http"
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
