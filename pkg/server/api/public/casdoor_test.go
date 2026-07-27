package public

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCasdoorConfigReturnsNewCallbackPath(t *testing.T) {
	require.Equal(t, "/api/casdoor", (&CasdoorConfig{}).RouterInfo().GetPath())
	require.Equal(t, "/api/casdoor/callback", casdoorCallbackPath())
	require.Equal(t, "/api/casdoor/callback?service=shop", casdoorCallbackPathForService(" Shop "))
	require.Equal(t, (&CasdoorConfig{}).RouterInfo().GetPath(), (&Casdoor{}).RouterInfo().GetPath())
}

func TestCasdoorConfigRejectsUnknownDomain(t *testing.T) {
	_, err := normalizeCasdoorAuthType("unknown")
	require.Error(t, err)
}
