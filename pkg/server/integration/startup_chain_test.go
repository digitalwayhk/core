// Package integration contains tests that exercise multiple subsystems together,
// verifying that config → factory → runtime wiring is consistent across modes.
//
// These tests do not require external services (Redis, NATS, etcd, consul).
// Mode=on paths that need real endpoints are skipped automatically.
package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// Cluster factory — mode compatibility
// ============================================================

// TestStartup_Cluster_ModeOff verifies that Mode=off returns nil provider
// without error; the service runs as a single node.
func TestStartup_Cluster_ModeOff(t *testing.T) {
	cfg := &config.ClusterConfig{Mode: "off"}
	cfg.ApplyDefaults()
	cfg.Mode = "off" // ApplyDefaults may set a default; override back

	local := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	prov, err := cluster.BuildProvider(cfg, local)
	require.NoError(t, err)
	assert.Nil(t, prov, "Mode=off must return nil provider")
}

// TestStartup_Cluster_ModeAuto_LocalProvider verifies that Mode=auto with
// Provider=local returns the shared LocalProvider.
func TestStartup_Cluster_ModeAuto_LocalProvider(t *testing.T) {
	cfg := &config.ClusterConfig{Mode: "auto", Provider: "local"}
	cfg.ApplyDefaults()

	local := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	prov, err := cluster.BuildProvider(cfg, local)
	require.NoError(t, err)
	assert.Same(t, local, prov, "Mode=auto/local should return the shared LocalProvider")
}

// TestStartup_Cluster_ModeAuto_UnavailableExternalFallsBackToLocal verifies
// that Mode=auto with an unavailable external provider (etcd with no endpoints)
// falls back to local without error.
func TestStartup_Cluster_ModeAuto_UnavailableExternalFallsBackToLocal(t *testing.T) {
	cfg := &config.ClusterConfig{Mode: "auto", Provider: "etcd"}
	cfg.ApplyDefaults()

	local := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	prov, err := cluster.BuildProvider(cfg, local)
	require.NoError(t, err)
	assert.Same(t, local, prov, "Mode=auto with unreachable etcd must degrade to local")
}

// TestStartup_Cluster_ModeOn_UnavailableProviderReturnsError verifies that
// Mode=on with an unconfigured external provider returns an error (no silent fallback).
func TestStartup_Cluster_ModeOn_UnavailableProviderReturnsError(t *testing.T) {
	cfg := &config.ClusterConfig{Mode: "on", Provider: "etcd"}
	cfg.ApplyDefaults()

	local := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	_, err := cluster.BuildProvider(cfg, local)
	require.Error(t, err, "Mode=on with unavailable provider must return an error")
}

// ============================================================
// Transport factory — mode compatibility
// ============================================================

// TestStartup_Transport_EmptyConfigReturnsNil verifies that an empty
// TransportConfig produces a nil selector (legacy HTTP path is used).
func TestStartup_Transport_EmptyConfigReturnsNil(t *testing.T) {
	sel, err := transport.BuildSelector(config.TransportConfig{})
	require.NoError(t, err)
	assert.Nil(t, sel, "empty transport config should return nil selector")
}

// TestStartup_Transport_GRPCPrimaryBuilds verifies grpc as primary transport.
func TestStartup_Transport_GRPCPrimaryBuilds(t *testing.T) {
	sel, err := transport.BuildSelector(config.TransportConfig{Internal: "grpc"})
	require.NoError(t, err)
	require.NotNil(t, sel)
}

// TestStartup_Transport_QUICPrimaryBuilds verifies that the newly added quic
// transport can be used as the primary.
func TestStartup_Transport_QUICPrimaryBuilds(t *testing.T) {
	sel, err := transport.BuildSelector(config.TransportConfig{Internal: "quic"})
	require.NoError(t, err)
	require.NotNil(t, sel)
}

// TestStartup_Transport_MQInternalStillErrors verifies mq is not yet implemented.
func TestStartup_Transport_MQInternalStillErrors(t *testing.T) {
	_, err := transport.BuildSelector(config.TransportConfig{Internal: "mq"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}

// TestStartup_Transport_FallbackOrderPreserved verifies that BuildSelector accepts
// a primary + multiple fallbacks and returns a non-nil selector without error.
// (Actual transport selection requires a live target and is covered by transport-level tests.)
func TestStartup_Transport_FallbackOrderPreserved(t *testing.T) {
	cfg := config.TransportConfig{Internal: "grpc", Fallback: []string{"quic", "http"}}
	sel, err := transport.BuildSelector(cfg)
	require.NoError(t, err)
	require.NotNil(t, sel, "grpc primary + quic,http fallbacks should build a non-nil selector")
}

// ============================================================
// MQ factory — mode compatibility
// ============================================================

// TestStartup_MQ_ModeOff verifies BuildManager returns nil without error.
func TestStartup_MQ_ModeOff(t *testing.T) {
	cfg := &config.MQConfig{Mode: "off"}
	mgr, err := mq.BuildManager(context.Background(), cfg)
	require.NoError(t, err)
	assert.Nil(t, mgr, "Mode=off should return nil manager")
}

// TestStartup_MQ_ModeAuto_UnavailableProviderReturnsNilWithoutError verifies
// that Mode=auto with a provider that cannot connect degrades gracefully.
func TestStartup_MQ_ModeAuto_UnavailableProviderReturnsNilWithoutError(t *testing.T) {
	cfg := &config.MQConfig{
		Mode:     "auto",
		Provider: "redis-stream",
		// No Redis address configured; provider will fail to connect.
	}
	cfg.ApplyDefaults()
	// Ensure ApplyDefaults didn't override Mode.
	cfg.Mode = "auto"

	mgr, err := mq.BuildManager(context.Background(), cfg)
	require.NoError(t, err, "Mode=auto with unavailable Redis should degrade, not error")
	assert.Nil(t, mgr)
}

// TestStartup_MQ_ModeOn_UnavailableProviderReturnsError verifies that
// Mode=on with an unconfigured provider returns an error.
func TestStartup_MQ_ModeOn_UnavailableProviderReturnsError(t *testing.T) {
	cfg := &config.MQConfig{
		Mode:     "on",
		Provider: "redis-stream",
	}
	cfg.ApplyDefaults()
	cfg.Mode = "on"

	_, err := mq.BuildManager(context.Background(), cfg)
	require.Error(t, err, "Mode=on with unavailable provider must return an error")
}

// ============================================================
// MQ transparent switch integration
// ============================================================

// TestStartup_MQ_TransparentSwitch_FullCycle exercises the full migration
// chain: BeginSwitch → Publish (dual-write) → CompleteSwitch → Publish (new only).
func TestStartup_MQ_TransparentSwitch_FullCycle(t *testing.T) {
	// Wire up two in-memory mock providers.
	oldProv := &mockMQProvider{name: "old-provider"}
	newProv := &mockMQProvider{name: "new-provider"}

	mgr := mq.NewManager()
	mgr.Register(oldProv)
	require.NoError(t, mgr.SetCurrent("old-provider"))

	ctx := context.Background()

	// Phase 1: begin switch — enters double-write stage.
	require.NoError(t, mgr.BeginSwitch(ctx, newProv, true))

	// Phase 2: publish during double-write — both providers must receive.
	require.NoError(t, mgr.Publish(ctx, "events", []byte("msg-a"), nil))
	assert.Len(t, oldProv.received, 1, "old provider must receive during double-write")
	assert.Len(t, newProv.received, 1, "new provider must receive during double-write")

	// Phase 3: complete switch — new provider becomes active; old is closed.
	require.NoError(t, mgr.CompleteSwitch(ctx, 0))

	assert.Equal(t, "new-provider", mgr.Current().Name())
	assert.True(t, oldProv.closed, "old provider should be closed after complete")
	assert.Nil(t, mgr.GetSwitcher(), "switcher should be cleared after completion")

	// Phase 4: publish after completion — only new provider receives.
	require.NoError(t, mgr.Publish(ctx, "events", []byte("msg-b"), nil))
	assert.Len(t, oldProv.received, 1, "old provider must not receive after switch completes")
	assert.Len(t, newProv.received, 2, "new provider must receive post-switch messages")
}

// TestStartup_MQ_TransparentSwitch_Rollback verifies that RollbackSwitch
// restores the old provider as active.
func TestStartup_MQ_TransparentSwitch_Rollback(t *testing.T) {
	oldProv := &mockMQProvider{name: "old-provider"}
	newProv := &mockMQProvider{name: "new-provider"}

	mgr := mq.NewManager()
	mgr.Register(oldProv)
	require.NoError(t, mgr.SetCurrent("old-provider"))

	ctx := context.Background()
	require.NoError(t, mgr.BeginSwitch(ctx, newProv, false))
	require.NoError(t, mgr.RollbackSwitch())

	assert.Equal(t, "old-provider", mgr.Current().Name())
	assert.True(t, newProv.closed, "new provider should be closed after rollback")
	assert.Nil(t, mgr.GetSwitcher())
}

// ============================================================
// helpers
// ============================================================

type mockMQProvider struct {
	name     string
	received [][]byte
	closed   bool
}

func (m *mockMQProvider) Name() string                    { return m.name }
func (m *mockMQProvider) Connect(_ context.Context) error { return nil }
func (m *mockMQProvider) Close() error                    { m.closed = true; return nil }
func (m *mockMQProvider) Health(_ context.Context) error  { return nil }
func (m *mockMQProvider) Publish(_ context.Context, _ string, data []byte, _ *mq.PublishOptions) error {
	m.received = append(m.received, data)
	return nil
}
func (m *mockMQProvider) Subscribe(_ context.Context, _ string, _ func(*mq.Message)) (func(), error) {
	return func() {}, nil
}
