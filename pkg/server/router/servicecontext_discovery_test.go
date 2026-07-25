package router

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
)

type closeTrackingDiscoveryProvider struct{ closes atomic.Int32 }

func (*closeTrackingDiscoveryProvider) Name() string                                      { return "close-tracking" }
func (*closeTrackingDiscoveryProvider) Register(context.Context, *cluster.NodeInfo) error { return nil }
func (*closeTrackingDiscoveryProvider) Deregister(context.Context, string) error          { return nil }
func (*closeTrackingDiscoveryProvider) Heartbeat(context.Context, string) error           { return nil }
func (*closeTrackingDiscoveryProvider) Get(context.Context, string) (*cluster.NodeInfo, error) {
	return nil, cluster.ErrNodeNotFound
}
func (*closeTrackingDiscoveryProvider) List(context.Context, string, ...cluster.NodeStatus) ([]*cluster.NodeInfo, error) {
	return nil, nil
}
func (*closeTrackingDiscoveryProvider) Watch(context.Context, string, func([]*cluster.NodeInfo)) (func(), error) {
	return func() {}, nil
}
func (p *closeTrackingDiscoveryProvider) Close() error { p.closes.Add(1); return nil }

func TestClusterMembershipUsesAdvertiseAddress(t *testing.T) {
	sc := &ServiceContext{
		Service: &types.Service{Name: "orders"},
		Config: &config.ServerConfig{
			Cluster: config.ClusterConfig{
				AdvertiseAddress: "orders.internal",
			},
		},
	}

	_, node, _ := sc.clusterMembershipConfig()
	assert.Equal(t, "orders.internal", node.Address)
}

func TestRuntimeAddressUsesCapturedAddressUnlessExplicitlyOverridden(t *testing.T) {
	sc := &ServiceContext{
		Config:         config.NewServiceDefaultConfig("orders", 0),
		runtimeAddress: "10.20.30.40",
	}
	assert.Equal(t, "10.20.30.40", sc.RuntimeAddress())

	sc.Config.Cluster.AdvertiseAddress = "orders.internal"
	assert.Equal(t, "orders.internal", sc.RuntimeAddress())
}

func TestServiceContextClosesOwnedDiscoveryProvider(t *testing.T) {
	provider := &closeTrackingDiscoveryProvider{}
	sc := &ServiceContext{
		Service:             &types.Service{Name: "orders"},
		Config:              config.NewServiceDefaultConfig("orders", 0),
		StateChan:           make(chan bool, 1),
		ClusterProvider:     provider,
		ownsClusterProvider: true,
	}

	sc.SetRunState(false)
	assert.Equal(t, int32(1), provider.closes.Load())
}
