package router_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeService is a minimal types.IService implementation used by lifecycle tests.
type fakeService struct{ name string }

func (f *fakeService) ServiceName() string                    { return f.name }
func (f *fakeService) Routers() []types.IRouter               { return nil }
func (f *fakeService) SubscribeRouters() []*types.ObserveArgs { return nil }

// TestNewServiceContext_SetsClusterProvider verifies that, with the default
// auto/local configuration, NewServiceContext produces a non-nil ClusterProvider.
func TestNewServiceContext_SetsClusterProvider(t *testing.T) {
	sc := router.NewServiceContext(&fakeService{"sctest-cluster-provider"})
	require.NotNil(t, sc)
	assert.NotNil(t, sc.ClusterProvider, "ClusterProvider should be set with default config")
}

// TestNewServiceContext_SetsTransportSelector verifies that the default gRPC
// transport configuration results in a non-nil TransportSelector.
func TestNewServiceContext_SetsTransportSelector(t *testing.T) {
	sc := router.NewServiceContext(&fakeService{"sctest-transport-selector"})
	require.NotNil(t, sc)
	assert.NotNil(t, sc.TransportSelector, "TransportSelector should be set with default transport config")
}

// TestNewServiceContext_ClusterSwitcherInitialCurrent verifies that the
// ClusterSwitcher's initial Current() provider is the same as ClusterProvider.
func TestNewServiceContext_ClusterSwitcherInitialCurrent(t *testing.T) {
	sc := router.NewServiceContext(&fakeService{"sctest-switcher-init"})
	require.NotNil(t, sc)
	require.NotNil(t, sc.ClusterSwitcher, "ClusterSwitcher should be initialised")
	assert.Equal(t, sc.ClusterProvider, sc.ClusterSwitcher.Current(),
		"ClusterSwitcher.Current() should equal ClusterProvider after initialisation")
}

// TestNewServiceContext_CachedByName verifies that the global scontext cache
// returns the same *ServiceContext for the same service name.
func TestNewServiceContext_CachedByName(t *testing.T) {
	svc := &fakeService{"sctest-cache"}
	sc1 := router.NewServiceContext(svc)
	sc2 := router.NewServiceContext(svc)
	assert.Same(t, sc1, sc2, "NewServiceContext should return the cached instance for the same name")
}

// TestSetRunState_TrueRegistersNode verifies that SetRunState(true) registers
// this service's node in the cluster provider so it appears in List().
func TestSetRunState_TrueRegistersNode(t *testing.T) {
	const svcName = "sctest-runstate-register"
	sc := router.NewServiceContext(&fakeService{svcName})
	require.NotNil(t, sc)

	sc.SetRunState(true)
	t.Cleanup(func() { sc.SetRunState(false) })

	// Allow the synchronous Register call inside SetRunState to complete.
	require.Eventually(t, func() bool {
		nodes, err := sc.ClusterProvider.List(context.Background(), svcName)
		if err != nil {
			return false
		}
		for _, n := range nodes {
			if n.ServiceName == svcName {
				return true
			}
		}
		return false
	}, 2*time.Second, 20*time.Millisecond,
		"node should appear in ClusterProvider after SetRunState(true)")
}

// TestSetRunState_FalseAfterTrue_CleansCrossNodeBroker verifies that
// SetRunState(false) stops and nils the CrossNodeBroker.
func TestSetRunState_FalseAfterTrue_CleansCrossNodeBroker(t *testing.T) {
	const svcName = "sctest-runstate-clean"
	sc := router.NewServiceContext(&fakeService{svcName})
	require.NotNil(t, sc)

	sc.SetRunState(true)
	time.Sleep(30 * time.Millisecond)

	// CrossNodeBroker should be created.
	assert.NotNil(t, sc.CrossNodeBroker, "CrossNodeBroker should be set after SetRunState(true)")

	sc.SetRunState(false)
	assert.Nil(t, sc.CrossNodeBroker, "CrossNodeBroker should be nil after SetRunState(false)")
}

// TestSyncProviderAfterSwitch_UpdatesClusterProvider verifies that after a
// Begin→Complete switch cycle, SyncProviderAfterSwitch updates ClusterProvider
// to the newly promoted provider.
func TestSyncProviderAfterSwitch_UpdatesClusterProvider(t *testing.T) {
	const svcName = "sctest-sync-provider"
	sc := router.NewServiceContext(&fakeService{svcName})
	require.NotNil(t, sc)
	require.NotNil(t, sc.ClusterSwitcher)

	// Create a fresh local provider to switch to.
	newProvider := cluster.NewLocalProvider(10*time.Second, 10*time.Second, 30*time.Second)
	newProvider.Start()
	t.Cleanup(func() { newProvider.Close() })

	ctx := context.Background()

	err := sc.ClusterSwitcher.Begin(ctx, newProvider)
	require.NoError(t, err)

	err = sc.ClusterSwitcher.Complete(ctx)
	require.NoError(t, err)

	err = sc.SyncProviderAfterSwitch()
	require.NoError(t, err)

	// Verify that ClusterProvider has been updated to the new provider.
	got, ok := sc.ClusterProvider.(*cluster.LocalProvider)
	require.True(t, ok, "ClusterProvider should still be a *cluster.LocalProvider")
	assert.Same(t, newProvider, got,
		"ClusterProvider should point to the newly promoted provider after sync")
}

// TestClusterConfig_ModeOnEmptyEndpoints_ValidateFails verifies that when
// Cluster.Mode=on and Provider=etcd but Endpoints is empty, Validate returns
// an error (which ReadConfig and NewServiceContext would panic on in production).
func TestClusterConfig_ModeOnEmptyEndpoints_ValidateFails(t *testing.T) {
	cfg := config.ClusterConfig{
		Mode:     "on",
		Provider: "etcd",
		// Providers.Etcd.Endpoints is left nil (empty)
	}
	cfg.ApplyDefaults()
	err := cfg.Validate()
	require.Error(t, err, "ClusterConfig.Validate should fail with Mode=on, Provider=etcd and no endpoints")
}

// TestTransportConfig_QuicInternal_ValidatePasses verifies that TransportConfig.Validate
// accepts "quic" as an Internal value (the panic would occur later in BuildSelector,
// not at validation time).
func TestTransportConfig_QuicInternal_ValidatePasses(t *testing.T) {
	cfg := config.TransportConfig{Internal: "quic"}
	cfg.ApplyDefaults()
	err := cfg.Validate()
	require.NoError(t, err, "TransportConfig.Validate should accept 'quic' (BuildSelector handles the panic)")
}

// TestNewServiceContextWithConfig_TransportMQ_Panics verifies that when
// Transport.Internal is set to "mq" (not yet implemented), BuildSelector
// returns an error and NewServiceContextWithConfig panics with a message
// that mentions both "mq" and "not implemented".
func TestNewServiceContextWithConfig_TransportMQ_Panics(t *testing.T) {
	const svcName = "sctest-panic-transport-mq"
	con := config.NewServiceDefaultConfig(svcName, 29986)
	con.Transport.Internal = "mq"

	var recovered interface{}
	didPanic := func() (panicked bool) {
		defer func() {
			recovered = recover()
			panicked = recovered != nil
		}()
		router.NewServiceContextWithConfig(&fakeService{svcName}, con)
		return false
	}()

	require.True(t, didPanic,
		"NewServiceContextWithConfig should panic when Transport.Internal=mq")

	panicMsg := fmt.Sprintf("%v", recovered)
	assert.Contains(t, panicMsg, "mq",
		"panic message should contain 'mq'")
	assert.Contains(t, panicMsg, "not implemented",
		"panic message should contain 'not implemented'")
}

// TestMQConfig_ModeOnEmptyUsage_ValidateFails verifies that when MQ.Mode=on and
// Usage is empty, Validate returns an error.
// Note: ApplyDefaults() fills Usage with a default, so this test calls Validate directly.
func TestMQConfig_ModeOnEmptyUsage_ValidateFails(t *testing.T) {
	cfg := config.MQConfig{Mode: "on", Provider: "redis-stream"}
	// Deliberately skip ApplyDefaults so Usage remains empty.
	err := cfg.Validate()
	require.Error(t, err, "MQConfig.Validate should fail when Mode=on and Usage is empty")
}

// TestSetRunState_FalseAfterTrue_DeregistersNode verifies that after SetRunState(false)
// the service node is removed from the cluster provider's running list.
func TestSetRunState_FalseAfterTrue_DeregistersNode(t *testing.T) {
	const svcName = "sctest-deregister-node"
	sc := router.NewServiceContext(&fakeService{svcName})
	require.NotNil(t, sc)

	sc.SetRunState(true)

	// Wait until node appears in the provider.
	require.Eventually(t, func() bool {
		nodes, _ := sc.ClusterProvider.List(context.Background(), svcName)
		for _, n := range nodes {
			if n.ServiceName == svcName {
				return true
			}
		}
		return false
	}, 2*time.Second, 20*time.Millisecond, "node should appear after SetRunState(true)")

	sc.SetRunState(false)

	// Node should no longer appear as running.
	nodes, err := sc.ClusterProvider.List(context.Background(), svcName, cluster.NodeStatusRunning)
	require.NoError(t, err)
	assert.Empty(t, nodes, "no running nodes should remain after SetRunState(false)")
}

// --- Real NewServiceContextWithConfig panic tests ---
// These tests bypass ReadConfig entirely, injecting a bad config to trigger
// the hard-panic paths inside initServiceContextPost.

// TestNewServiceContextWithConfig_ClusterModeOn_Panics verifies that when
// Cluster.Mode="on" and the required etcd provider has no endpoints,
// NewServiceContextWithConfig panics (hard misconfiguration, cannot degrade).
func TestNewServiceContextWithConfig_ClusterModeOn_Panics(t *testing.T) {
	const svcName = "sctest-panic-cluster"
	con := config.NewServiceDefaultConfig(svcName, 29990)
	con.Cluster.Mode = "on"
	con.Cluster.Provider = "etcd"
	con.Cluster.Providers.Etcd.Endpoints = []string{} // empty → NewEtcdProvider fails immediately

	assert.Panics(t, func() {
		router.NewServiceContextWithConfig(&fakeService{svcName}, con)
	}, "NewServiceContextWithConfig should panic when Cluster.Mode=on and etcd has no endpoints")
}

// TestNewServiceContextWithConfig_TransportQuic_Panics verifies that when
// Transport.Internal is set to an unsupported protocol ("quic"), BuildSelector
// returns an error and NewServiceContextWithConfig panics.
func TestNewServiceContextWithConfig_TransportQuic_Panics(t *testing.T) {
	const svcName = "sctest-panic-transport"
	con := config.NewServiceDefaultConfig(svcName, 29989)
	con.Transport.Internal = "quic" // not in builders map → BuildSelector error

	assert.Panics(t, func() {
		router.NewServiceContextWithConfig(&fakeService{svcName}, con)
	}, "NewServiceContextWithConfig should panic when Transport.Internal is an unsupported protocol")
}

// TestNewServiceContextWithConfig_MQModeOn_Panics verifies that when
// MQ.Mode="on" and the redis-stream provider address is unreachable,
// NewServiceContextWithConfig panics.
func TestNewServiceContextWithConfig_MQModeOn_Panics(t *testing.T) {
	const svcName = "sctest-panic-mq"
	con := config.NewServiceDefaultConfig(svcName, 29988)
	con.MQ.Mode = "on"
	con.MQ.Provider = "redis-stream"
	con.MQ.RedisStream.Addr = "127.0.0.1:0" // invalid port → Connect fails immediately
	con.MQ.Usage = []string{"event"}        // non-empty usage to pass Validate

	assert.Panics(t, func() {
		router.NewServiceContextWithConfig(&fakeService{svcName}, con)
	}, "NewServiceContextWithConfig should panic when MQ.Mode=on and provider is unreachable")
}
