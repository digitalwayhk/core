package router

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

// TestClaimMachineIDAutoUsesClusterProvider 验证 AutoMachineID 使用当前 Provider 分配同名服务副本。
func TestClaimMachineIDAutoUsesClusterProvider(t *testing.T) {
	provider := cluster.NewLocalProvider(10*time.Second, 10*time.Second, 30*time.Second)
	provider.Start()
	t.Cleanup(func() { require.NoError(t, provider.Close()) })

	firstConfig := autoMachineIDConfig(19001)
	first := autoMachineIDContext("orders", "orders-instance-a", firstConfig, provider)
	firstID, err := claimMachineID(first, firstConfig)
	require.NoError(t, err)

	secondConfig := autoMachineIDConfig(19002)
	second := autoMachineIDContext("orders", "orders-instance-b", secondConfig, provider)
	secondID, err := claimMachineID(second, secondConfig)
	require.NoError(t, err)

	require.NotEqual(t, firstID, secondID)

	nodes, err := provider.List(context.Background(), "orders", cluster.NodeStatusRunning)
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	seen := map[string]bool{}
	for _, node := range nodes {
		seen[node.ServiceInstanceID] = true
	}
	require.True(t, seen["orders-instance-a"])
	require.True(t, seen["orders-instance-b"])
}

// TestClaimMachineIDAutoFailsWhenSlotsFull 验证 AutoMachineID 槽位耗尽时 fail closed。
func TestClaimMachineIDAutoFailsWhenSlotsFull(t *testing.T) {
	provider := cluster.NewLocalProvider(10*time.Second, 10*time.Second, 30*time.Second)
	provider.Start()
	t.Cleanup(func() { require.NoError(t, provider.Close()) })

	firstConfig := autoMachineIDConfig(19101)
	firstConfig.Cluster.Claim.MachineIDMax = 1
	first := autoMachineIDContext("orders-full", "orders-full-a", firstConfig, provider)
	_, err := claimMachineID(first, firstConfig)
	require.NoError(t, err)

	secondConfig := autoMachineIDConfig(19102)
	secondConfig.Cluster.Claim.MachineIDMax = 1
	second := autoMachineIDContext("orders-full", "orders-full-b", secondConfig, provider)
	_, err = claimMachineID(second, secondConfig)
	require.NoError(t, err)

	thirdConfig := autoMachineIDConfig(19103)
	thirdConfig.Cluster.Claim.MachineIDMax = 1
	third := autoMachineIDContext("orders-full", "orders-full-c", thirdConfig, provider)
	_, err = claimMachineID(third, thirdConfig)
	require.Error(t, err)
}

func autoMachineIDConfig(port int) *config.ServerConfig {
	cfg := config.NewServiceDefaultConfig("orders", port)
	cfg.DataCenterID = 1
	cfg.MachineID = 0
	cfg.Cluster.Mode = "auto"
	cfg.Cluster.Provider = "local"
	cfg.Cluster.Claim.AutoMachineID = true
	cfg.Cluster.Claim.MachineIDMax = 3
	cfg.Cluster.ApplyDefaults()
	return cfg
}

func autoMachineIDContext(serviceName, instanceID string, cfg *config.ServerConfig, provider cluster.DiscoveryProvider) *ServiceContext {
	return &ServiceContext{
		Config:            cfg,
		Service:           &types.Service{Name: serviceName},
		ServiceInstanceID: instanceID,
		ClusterProvider:   provider,
	}
}
