package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestNewEtcdProvider_DefaultPrefixMatchesConfig(t *testing.T) {
	provider, err := NewEtcdProvider([]string{"127.0.0.1:2379"}, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, provider.Close()) })

	assert.Equal(t, config.DefaultClusterEtcdPrefix, provider.prefix)
	assert.Equal(t, "/core/cluster/orders/node-1", provider.nodeKey("orders", "node-1"))
}

func TestNewEtcdProviderWithPrefix_CustomPrefix(t *testing.T) {
	provider, err := NewEtcdProviderWithPrefix([]string{"127.0.0.1:2379"}, time.Second, "/tenant-a/discovery")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, provider.Close()) })

	assert.Equal(t, "/tenant-a/discovery", provider.prefix)
	assert.Equal(t, "/tenant-a/discovery/orders/node-1", provider.nodeKey("orders", "node-1"))
	assert.Equal(t, "/tenant-a/discovery/orders/", provider.servicePrefix("orders"), "List and Watch must share the configured service prefix")
	assert.NotNil(t, provider.get)
	assert.NotNil(t, provider.watch)
}

func TestEtcdProviderList_UsesConfiguredServicePrefix(t *testing.T) {
	var gotKey string
	var gotPrefixOption bool
	provider := &EtcdProvider{
		prefix: "/tenant-a/discovery",
		get: func(_ context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			gotKey = key
			gotPrefixOption = clientv3.IsOptsWithPrefix(opts)
			return &clientv3.GetResponse{}, nil
		},
	}

	nodes, err := provider.List(context.Background(), "orders")
	require.NoError(t, err)
	assert.Empty(t, nodes)
	assert.Equal(t, "/tenant-a/discovery/orders/", gotKey)
	assert.True(t, gotPrefixOption)
}

func TestEtcdProviderWatch_UsesConfiguredServicePrefixAndCancels(t *testing.T) {
	var gotKey string
	var gotPrefixOption bool
	watchStarted := make(chan struct{})
	watchStopped := make(chan struct{})
	provider := &EtcdProvider{
		prefix: "/tenant-a/discovery",
		watch: func(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan {
			gotKey = key
			gotPrefixOption = clientv3.IsOptsWithPrefix(opts)
			watchCh := make(chan clientv3.WatchResponse)
			close(watchStarted)
			go func() {
				<-ctx.Done()
				close(watchCh)
				close(watchStopped)
			}()
			return watchCh
		},
	}

	cancel, err := provider.Watch(context.Background(), "orders", func([]*NodeInfo) {})
	require.NoError(t, err)
	<-watchStarted
	assert.Equal(t, "/tenant-a/discovery/orders/", gotKey)
	assert.True(t, gotPrefixOption)

	cancel()
	select {
	case <-watchStopped:
	case <-time.After(time.Second):
		t.Fatal("watch dependency did not stop after cancellation")
	}
}

func TestNewEtcdProvider_MalformedEndpointFails(t *testing.T) {
	provider, err := NewEtcdProvider([]string{"%gh"}, time.Second)
	require.Error(t, err)
	assert.Nil(t, provider)
}

func TestBuildProvider_PassesConfiguredEtcdPrefix(t *testing.T) {
	sharedLocal := NewLocalProvider(time.Second, time.Second, time.Second)
	provider, err := BuildProvider(&config.ClusterConfig{
		Mode:     "on",
		Provider: "etcd",
		Providers: config.ClusterProviderConfig{
			Etcd: config.EtcdProviderConfig{
				Endpoints: []string{"127.0.0.1:2379"},
				Prefix:    "/tenant-a/discovery",
			},
		},
	}, sharedLocal)
	require.NoError(t, err)

	etcdProvider, ok := provider.(*EtcdProvider)
	require.True(t, ok)
	t.Cleanup(func() { require.NoError(t, etcdProvider.Close()) })
	assert.Equal(t, "/tenant-a/discovery", etcdProvider.prefix)
}
