package transport

import (
	"context"
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
	sel, err := BuildSelector(config.TransportConfig{Internal: "grpc", Fallback: []string{"http"}})
	require.NoError(t, err)
	require.NotNil(t, sel)

	ds, ok := sel.(*DefaultSelector)
	require.True(t, ok)
	require.Len(t, ds.fallback, 1)
	assert.Equal(t, "grpc", ds.primary.Name())
	assert.Equal(t, "http", ds.fallback[0].Name())
}

func TestBuildSelector_RemovedSocketReturnsError(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{Internal: "socket"})
	assert.Nil(t, sel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "supported protocols: grpc, http")
}

func TestBuildSelector_GRPCInjectsSecurityConfig(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{
		Internal: "grpc",
		GRPC: config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{
			Mode: "mtls", CAFile: "/missing/ca.pem", CertFile: "/missing/client.pem", KeyFile: "/missing/client.key",
		}},
	})
	require.NoError(t, err)
	ds := sel.(*DefaultSelector)
	err = ds.primary.Health(context.Background(), "127.0.0.1:19090")
	require.ErrorContains(t, err, "Transport.GRPC.Security.CAFile")
}

func TestBuildSelector_EmptyConfig(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{})
	assert.NoError(t, err)
	assert.Nil(t, sel)
}

// --- Unimplemented protocol tests (R3) ---

func TestBuildSelector_QuicInternalReturnsError(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{Internal: "quic"})
	assert.Nil(t, sel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quic")
	assert.Contains(t, err.Error(), "not implemented")
}

func TestBuildSelector_MQInternalReturnsError(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{Internal: "mq"})
	assert.Nil(t, sel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mq")
	assert.Contains(t, err.Error(), "not implemented")
}

func TestBuildSelector_UnimplementedFallbackReturnsError(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{
		Internal: "grpc",
		Fallback: []string{"mq", "http"},
	})
	assert.Nil(t, sel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mq")
	assert.Contains(t, err.Error(), "not implemented")
}

// TestBuildSelector_AllFallbackUnimplementedReturnsError verifies that when every
// fallback entry is unimplemented and Internal is empty, BuildSelector returns an
// error rather than (nil, nil).
func TestBuildSelector_AllFallbackUnimplementedReturnsError(t *testing.T) {
	sel, err := BuildSelector(config.TransportConfig{
		Fallback: []string{"mq", "quic"},
	})
	assert.Nil(t, sel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mq")
	assert.Contains(t, err.Error(), "not implemented")
}
