package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	evtpkg "github.com/digitalwayhk/core/pkg/server/event"
	mqpkg "github.com/digitalwayhk/core/pkg/server/mq"
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

// ---------- EventBridge tests ----------

// syncMQProvider is an in-memory MQProvider for testing EnableEventBridge.
// Publish synchronously calls any registered subscriber handler.
type syncMQProvider struct {
	handler func(msg *mqpkg.Message)
}

func (p *syncMQProvider) Name() string                    { return "sync-fake" }
func (p *syncMQProvider) Connect(_ context.Context) error { return nil }
func (p *syncMQProvider) Close() error                    { return nil }
func (p *syncMQProvider) Health(_ context.Context) error  { return nil }
func (p *syncMQProvider) Publish(_ context.Context, subject string, data []byte, _ *mqpkg.PublishOptions) error {
	if p.handler != nil {
		p.handler(&mqpkg.Message{
			Subject: subject,
			Data:    data,
			Ack:     func() error { return nil },
		})
	}
	return nil
}
func (p *syncMQProvider) Subscribe(_ context.Context, _ string, handler func(*mqpkg.Message)) (func(), error) {
	p.handler = handler
	return func() { p.handler = nil }, nil
}

// TestEventBridge_PublishSubscribeRoundtrip verifies the framework path:
// ServiceContext.EnableEventBridge creates a non-nil EventStream and EventBridge,
// and an event.Envelope can be published via MQBridge then consumed through the
// in-process Stream.
func TestEventBridge_PublishSubscribeRoundtrip(t *testing.T) {
	provider := &syncMQProvider{}
	mgr := mqpkg.NewManager()
	mgr.Register(provider)
	require.NoError(t, mgr.SetCurrent("sync-fake"))

	sc := &router.ServiceContext{MQManager: mgr}
	sc.EnableEventBridge()

	require.NotNil(t, sc.EventStream, "EnableEventBridge should set EventStream")
	require.NotNil(t, sc.EventBridge, "EnableEventBridge should set EventBridge")

	ctx := context.Background()
	const subject = "test.events"

	// Subscribe on both the MQ bridge path and the in-process stream.
	received := make(chan *evtpkg.Envelope, 1)
	sc.EventStream.Subscribe("test-type", func(env *evtpkg.Envelope) {
		received <- env
	})
	_, err := sc.EventBridge.Subscribe(ctx, subject)
	require.NoError(t, err)

	env := &evtpkg.Envelope{
		Type: "test-type",
		ID:   "evt-roundtrip-001",
		Data: []byte(`"hello bridge"`),
	}
	require.NoError(t, sc.EventBridge.Publish(ctx, subject, env))

	select {
	case got := <-received:
		assert.Equal(t, env.ID, got.ID, "roundtrip envelope ID should match")
		assert.Equal(t, env.Type, got.Type, "roundtrip envelope Type should match")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event bridge roundtrip")
	}
}

// TestNewServiceContextWithConfig_EventBridgeAutoInit verifies that when
// NewServiceContextWithConfig is called with MQ.Usage containing "event-stream",
// the framework automatically wires sc.MQManager, sc.EventStream, and sc.EventBridge.
// A fake synchronous MQ provider is injected via RegisterProviderFactory to avoid
// requiring a real Redis/NATS server.
func TestNewServiceContextWithConfig_EventBridgeAutoInit(t *testing.T) {
	const (
		svcName      = "sctest-event-bridge-autoinit"
		providerName = "fake-sync-autoinit"
	)

	// Register a fake in-process provider so BuildManager can succeed without
	// a real MQ broker.
	mqpkg.RegisterProviderFactory(providerName, func(_ context.Context, _ *config.MQConfig) (mqpkg.MQProvider, error) {
		return &syncMQProvider{}, nil
	})

	con := config.NewServiceDefaultConfig(svcName, 29984)
	con.MQ.Mode = "on"
	con.MQ.Provider = providerName
	con.MQ.Usage = []string{"event-stream"}

	sc := router.NewServiceContextWithConfig(&fakeService{svcName}, con)
	require.NotNil(t, sc)

	assert.NotNil(t, sc.MQManager, "MQManager should be initialized via fake provider")
	assert.NotNil(t, sc.EventStream, "EventStream should be auto-wired when usage contains event-stream")
	assert.NotNil(t, sc.EventBridge, "EventBridge should be auto-wired when usage contains event-stream")

	// Verify a full Publish → Subscribe roundtrip through the auto-wired bridge.
	ctx := context.Background()
	const subject = "test.autoinit.events"

	received := make(chan *evtpkg.Envelope, 1)
	sc.EventStream.Subscribe("autoinit-type", func(env *evtpkg.Envelope) {
		received <- env
	})
	_, err := sc.EventBridge.Subscribe(ctx, subject)
	require.NoError(t, err)

	env := &evtpkg.Envelope{
		Type: "autoinit-type",
		ID:   "evt-autoinit-001",
		Data: []byte(`"hello auto-bridge"`),
	}
	require.NoError(t, sc.EventBridge.Publish(ctx, subject, env))

	select {
	case got := <-received:
		assert.Equal(t, env.ID, got.ID, "auto-init roundtrip envelope ID should match")
		assert.Equal(t, env.Type, got.Type, "auto-init roundtrip envelope Type should match")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for auto-init event bridge roundtrip")
	}
}

// TestNewServiceContext_ReadConfigInitializesRuntimeSubsystems verifies the
// production bootstrap path: NewServiceContext reads the persisted service
// config and initialises cluster, transport, MQ/event-stream, and the
// cross-node broker when the service is marked running.
func TestNewServiceContext_ReadConfigInitializesRuntimeSubsystems(t *testing.T) {
	const svcName = "sctest-readconfig-runtime"

	mqpkg.RegisterProviderFactory("kafka", func(_ context.Context, _ *config.MQConfig) (mqpkg.MQProvider, error) {
		return &syncMQProvider{}, nil
	})

	oldConfigDir := config.CONFIGDIRPATH
	config.CONFIGDIRPATH = t.TempDir() + string(os.PathSeparator)
	t.Cleanup(func() {
		config.CONFIGDIRPATH = oldConfigDir
	})

	con := config.NewServiceDefaultConfig(svcName, 29983)
	con.Cluster.Mode = "auto"
	con.Cluster.Provider = "local"
	con.Transport.Internal = "http"
	con.Transport.Fallback = []string{"http"}
	con.MQ.Mode = "on"
	con.MQ.Provider = "kafka"
	con.MQ.Usage = []string{"event-stream"}
	con.MQ.Kafka.Brokers = []string{"local-test"}
	con.ApplyDefaults()
	require.NoError(t, con.Validate())

	data := mustMarshalConfigForReadConfig(t, con)
	require.NoError(t, os.WriteFile(filepath.Join(config.CONFIGDIRPATH, svcName+".json"), data, 0o600))

	sc := router.NewServiceContext(&fakeService{svcName})
	require.NotNil(t, sc)
	require.NotNil(t, sc.ClusterProvider, "NewServiceContext should build ClusterProvider from file config")
	require.NotNil(t, sc.TransportSelector, "NewServiceContext should build TransportSelector from file config")
	require.NotNil(t, sc.MQManager, "NewServiceContext should build MQManager from file config")
	require.NotNil(t, sc.EventStream, "NewServiceContext should create EventStream for event-stream usage")
	require.NotNil(t, sc.EventBridge, "NewServiceContext should wire EventBridge for event-stream usage")

	sc.SetRunState(true)
	t.Cleanup(func() {
		sc.SetRunState(false)
	})
	require.NotNil(t, sc.CrossNodeBroker, "SetRunState(true) should start CrossNodeNoticeBroker when ClusterProvider exists")
	assert.NotNil(t, types.GetCrossNodeForwarder(), "cross-node forwarder should be globally registered while running")
}

func mustMarshalConfigForReadConfig(t *testing.T, con *config.ServerConfig) []byte {
	t.Helper()
	data, err := json.Marshal(con)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))

	setConfigString(m, con.Profiling.UploadRate.String(), "Profiling", "UploadRate")
	setConfigString(m, con.Profiling.CheckInterval.String(), "Profiling", "CheckInterval")
	setConfigString(m, con.Profiling.ProfilingDuration.String(), "Profiling", "ProfilingDuration")
	setConfigString(m, con.Shutdown.WrapUpTime.String(), "Shutdown", "WrapUpTime")
	setConfigString(m, con.Shutdown.WaitTime.String(), "Shutdown", "WaitTime")
	setConfigString(m, fmt.Sprintf("%dh", int(con.Signature.Expiry/time.Hour)), "Signature", "Expiry")
	setConfigValue(m, []interface{}{}, "Signature", "PrivateKeys")

	setConfigString(m, con.Cluster.HeartbeatInterval.String(), "Cluster", "HeartbeatInterval")
	setConfigString(m, con.Cluster.HeartbeatTimeout.String(), "Cluster", "HeartbeatTimeout")
	setConfigString(m, con.Cluster.SuspectTimeout.String(), "Cluster", "SuspectTimeout")
	setConfigString(m, con.Cluster.InstanceReuseCooldown.String(), "Cluster", "InstanceReuseCooldown")
	setConfigString(m, con.Transport.RetryDelay.String(), "Transport", "RetryDelay")
	setConfigString(m, con.Cluster.Providers.Etcd.TTL.String(), "Cluster", "Providers", "Etcd", "TTL")
	setConfigString(m, con.Cluster.Providers.Consul.TTL.String(), "Cluster", "Providers", "Consul", "TTL")

	setConfigString(m, con.MQ.RequestReply.Timeout.String(), "MQ", "RequestReply", "Timeout")
	setConfigString(m, con.MQ.Retry.InitialDelay.String(), "MQ", "Retry", "InitialDelay")
	setConfigString(m, con.MQ.Retry.MaxDelay.String(), "MQ", "Retry", "MaxDelay")
	setConfigString(m, con.MQ.Switch.DualWriteDuration.String(), "MQ", "Switch", "DualWriteDuration")

	data, err = json.Marshal(m)
	require.NoError(t, err)
	return data
}

func setConfigString(m map[string]interface{}, value string, path ...string) {
	setConfigValue(m, value, path...)
}

func setConfigValue(m map[string]interface{}, value interface{}, path ...string) {
	if len(path) == 0 {
		return
	}
	cur := m
	for _, key := range path[:len(path)-1] {
		next, ok := cur[key].(map[string]interface{})
		if !ok {
			return
		}
		cur = next
	}
	cur[path[len(path)-1]] = value
}
