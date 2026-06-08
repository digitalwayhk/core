package transport

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSelector_HTTPPrimary(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{Internal: "http"})
	require.NoError(t, err)
	require.NotNil(t, sel)

	ds, ok := sel.(*DefaultSelector)
	require.True(t, ok)
	assert.Equal(t, "http", ds.primary.Name())
	assert.Empty(t, ds.fallback)
}

func TestBuildSelector_GRPCWithHTTPFallback(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{Internal: "grpc", Fallback: []string{"http", "socket"}})
	require.NoError(t, err)
	require.NotNil(t, sel)

	ds, ok := sel.(*DefaultSelector)
	require.True(t, ok)
	require.Len(t, ds.fallback, 2)
	assert.Equal(t, "grpc", ds.primary.Name())
	assert.Equal(t, "http", ds.fallback[0].Name())
	assert.Equal(t, "socket", ds.fallback[1].Name())
}

func TestBuildSelector_EmptyConfig(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{})
	assert.NoError(t, err)
	assert.Nil(t, sel)
}

// TestBuildSelector_QUICPrimary verifies that "quic" is now a fully registered
// protocol and produces a valid selector.
func TestBuildSelector_QUICPrimary(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{Internal: "quic"})
	require.NoError(t, err)
	require.NotNil(t, sel)

	ds, ok := sel.(*DefaultSelector)
	require.True(t, ok)
	assert.Equal(t, "quic", ds.primary.Name())
	assert.Empty(t, ds.fallback)
}

func TestBuildSelector_QUICFallback(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{
		Internal: "grpc",
		Fallback: []string{"quic", "http"},
	})
	require.NoError(t, err)
	require.NotNil(t, sel)

	ds, ok := sel.(*DefaultSelector)
	require.True(t, ok)
	assert.Equal(t, "grpc", ds.primary.Name())
	require.Len(t, ds.fallback, 2)
	assert.Equal(t, "quic", ds.fallback[0].Name())
	assert.Equal(t, "http", ds.fallback[1].Name())
}

func TestBuildSelector_MQInternalReturnsError(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{Internal: "mq"})
	assert.Nil(t, sel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mq")
	assert.Contains(t, err.Error(), "not implemented")
}

// TestBuildSelector_FallbackMQSkippedOrderPreserved verifies that an unimplemented
// fallback entry ("mq") is skipped with a warning while the implemented entries
// (grpc, http) are built in their configured order.
func TestBuildSelector_FallbackMQSkippedOrderPreserved(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{
		Internal: "grpc",
		Fallback: []string{"mq", "http"},
	})
	require.NoError(t, err)
	require.NotNil(t, sel)

	ds, ok := sel.(*DefaultSelector)
	require.True(t, ok)
	assert.Equal(t, "grpc", ds.primary.Name())
	require.Len(t, ds.fallback, 1, "mq should be skipped; only http remains in fallback")
	assert.Equal(t, "http", ds.fallback[0].Name())
}

// TestBuildSelector_AllFallbackUnimplementedReturnsError verifies that when every
// fallback entry is unimplemented and Internal is empty, BuildSelector returns an
// error rather than (nil, nil).
func TestBuildSelector_AllFallbackUnimplementedReturnsError(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{
		Fallback: []string{"mq"},
	})
	assert.Nil(t, sel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no implemented transports")
}

