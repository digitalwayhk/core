// Package shoporderscalemultiprocess 验证 07 多进程水平扩展的配置约束。
// 本文件不启动外部 Redis，而是锁定自动 MachineID、服务逻辑名和发现 Provider 可配置这些扩容前提。
package shoporderscalemultiprocess

import (
	"testing"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/bootstrap"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/require"
)

// TestOrderReplicaUsesAutoMachineID 验证 order 副本使用 AutoMachineID 而不是硬编码 MachineID。
func TestOrderReplicaUsesAutoMachineID(t *testing.T) {
	cfg := bootstrap.DistributedOrderConfig(18183, 4)
	require.Equal(t, contract.OrderServiceName, cfg.Name)
	require.True(t, cfg.Cluster.Claim.AutoMachineID)
	require.Zero(t, cfg.MachineID)
	require.GreaterOrEqual(t, cfg.Cluster.Claim.MachineIDMax, uint(3))
	require.Equal(t, "on", cfg.Cluster.Mode)
	require.NotEmpty(t, cfg.Cluster.Provider)
	require.Equal(t, "grpc", cfg.Transport.Internal)
	require.Empty(t, cfg.Transport.Fallback)
}

// TestDiscoveryProviderComesFromConfig 验证 07 只通过配置选择发现 Provider，业务服务不写死发现实现。
func TestDiscoveryProviderComesFromConfig(t *testing.T) {
	local := bootstrap.LocalServiceConfig(contract.OrderServiceName, 18183, 4, 3)
	distributed := bootstrap.DistributedOrderConfig(18183, 4)
	require.Equal(t, "local", local.Cluster.Provider)
	require.Equal(t, "redis", distributed.Cluster.Provider)
	require.Equal(t, "insecure", local.Transport.GRPC.Security.Mode)
	require.Equal(t, "mesh", distributed.Transport.GRPC.Security.Mode)
	require.Equal(t, "127.0.0.1:6379", distributed.Cluster.Providers.Redis.Addr)
	require.Equal(t, "127.0.0.1:6379", distributed.MQ.RedisStream.Addr)
}

// TestOrderReplicaPortsComeFromEnvironment 验证 order 副本端口可由编排环境覆盖。
func TestOrderReplicaPortsComeFromEnvironment(t *testing.T) {
	t.Setenv("SHOP_ORDER_HTTP_PORT", "19083")
	t.Setenv("SHOP_ORDER_GRPC_PORT", "29083")
	require.Equal(t, 19083, bootstrap.OrderHTTPPort())
	cfg := bootstrap.DistributedOrderConfig(bootstrap.OrderHTTPPort(), 4)
	require.Equal(t, 19083, cfg.Port)
	require.Equal(t, 29083, cfg.Transport.GRPC.Port)
}

// TestOrderReplicaNewIDDoesNotCollide 验证不同 MachineID 副本生成的订单 ID 不重复。
func TestOrderReplicaNewIDDoesNotCollide(t *testing.T) {
	first := utils.NewAlgorithmSnowFlake(1, 4)
	second := utils.NewAlgorithmSnowFlake(2, 4)
	seen := make(map[uint]struct{}, 2000)
	for index := 0; index < 1000; index++ {
		for _, id := range []uint{uint(first.NextId()), uint(second.NextId())} {
			if _, exists := seen[id]; exists {
				t.Fatalf("水平副本生成重复 ID: %d", id)
			}
			seen[id] = struct{}{}
		}
	}
}
