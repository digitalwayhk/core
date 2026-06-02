package transport

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSelector_HTTPPrimary(t *testing.T) {
	sel := BuildSelector(config.TransportConfig{Internal: "http"})
	require.NotNil(t, sel)

	ds, ok := sel.(*DefaultSelector)
	require.True(t, ok)
	assert.Equal(t, "http", ds.primary.Name())
	assert.Empty(t, ds.fallback)
}

func TestBuildSelector_GRPCWithHTTPFallback(t *testing.T) {
	sel := BuildSelector(config.TransportConfig{Internal: "grpc", Fallback: []string{"http", "socket"}})
	require.NotNil(t, sel)

	ds, ok := sel.(*DefaultSelector)
	require.True(t, ok)
	require.Len(t, ds.fallback, 2)
	assert.Equal(t, "grpc", ds.primary.Name())
	assert.Equal(t, "http", ds.fallback[0].Name())
	assert.Equal(t, "socket", ds.fallback[1].Name())
}

func TestBuildSelector_EmptyConfig(t *testing.T) {
	assert.Nil(t, BuildSelector(config.TransportConfig{}))
}
