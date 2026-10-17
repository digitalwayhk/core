// 本文件验证示例发现后端可独立选择，不改变事件与编号后端。
package bootstrap

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDistributedEtcdDiscovery 验证显式 etcd 端点只替换发现配置。
func TestDistributedEtcdDiscovery(t *testing.T) {
	t.Setenv("SHOP_ETCD_DISCOVERY_ENDPOINT", "127.0.0.1:12389")
	t.Setenv("SHOP_REDIS_DISCOVERY_PREFIX", "test-discovery")
	c := DistributedServiceConfig("shop-order", 18003, 4, 1)
	require.Equal(t, "etcd", c.Cluster.Provider)
	require.Equal(t, []string{"127.0.0.1:12389"}, c.Cluster.Providers.Etcd.Endpoints)
	require.Equal(t, "test-discovery", c.Cluster.Providers.Etcd.Prefix)
	require.Equal(t, "redis-stream", c.MQ.Provider)
}
