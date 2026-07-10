package rest

import (
	"testing"

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
