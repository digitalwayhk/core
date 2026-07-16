package router

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
)

func TestClusterMembershipUsesAdvertiseAddress(t *testing.T) {
	sc := &ServiceContext{
		Service: &types.Service{Name: "orders"},
		Config: &config.ServerConfig{
			RunIp: "0.0.0.0",
			Cluster: config.ClusterConfig{
				AdvertiseAddress: "orders.internal",
			},
		},
	}

	_, node, _ := sc.clusterMembershipConfig()
	assert.Equal(t, "orders.internal", node.Address)
}
