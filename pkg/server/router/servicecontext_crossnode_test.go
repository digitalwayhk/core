package router_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func crossNodeServiceConfig(name string, port int) *config.ServerConfig {
	con := config.NewServiceDefaultConfig(name, port)
	con.Cluster.Mode = "auto"
	con.Cluster.Provider = "local"
	con.MQ.Mode = "off"
	con.Transport.Internal = ""
	con.Transport.Fallback = nil
	return con
}

func TestServiceContextsKeepCrossNodeForwardersIsolated(t *testing.T) {
	prefix := fmt.Sprintf("sctest-cross-node-%d", time.Now().UnixNano())
	serviceA := prefix + "-a"
	serviceB := prefix + "-b"
	scA := router.NewServiceContextWithConfig(
		&fakeService{serviceA},
		crossNodeServiceConfig(serviceA, 32101),
	)
	scB := router.NewServiceContextWithConfig(
		&fakeService{serviceB},
		crossNodeServiceConfig(serviceB, 32102),
	)
	require.NotNil(t, scA.ClusterProvider)
	require.NotNil(t, scB.ClusterProvider)

	scA.SetRunState(true)
	scB.SetRunState(true)
	t.Cleanup(func() {
		scA.SetRunState(false)
		scB.SetRunState(false)
	})

	require.Same(t, scA.CrossNodeBroker, types.GetCrossNodeForwarderForService(serviceA))
	require.Same(t, scB.CrossNodeBroker, types.GetCrossNodeForwarderForService(serviceB))

	brokerB := scB.CrossNodeBroker
	scA.SetRunState(false)
	require.Nil(t, types.GetCrossNodeForwarderForService(serviceA))
	require.Same(t, brokerB, types.GetCrossNodeForwarderForService(serviceB))
}
