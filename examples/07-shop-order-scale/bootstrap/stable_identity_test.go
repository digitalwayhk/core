// 本文件验证示例显式选择稳定身份与隔离发现前缀。
package bootstrap

import (
 "path/filepath"
 "testing"
 "github.com/stretchr/testify/require"
)
func TestStableIdentityEnvironment(t *testing.T) {
 root:=t.TempDir()
 t.Setenv("SHOP_IDENTITY_ROOT",root)
 t.Setenv("SHOP_GENERATOR_REDIS_ADDR","127.0.0.1:16389")
 t.Setenv("SHOP_GENERATOR_NAMESPACE","test")
 t.Setenv("SHOP_GENERATOR_EPOCH","epoch-a")
 t.Setenv("SHOP_REDIS_DISCOVERY_PREFIX","core:test:isolated")
 c:=DistributedOrderConfig(18083,4)
 require.Equal(t,"stable",c.Cluster.IdentityMode)
 require.Equal(t,filepath.Join(root,"shop-order","identity.json"),c.Cluster.IdentityFile)
 require.Equal(t,"core:test:isolated",c.Cluster.Providers.Redis.Prefix)
 require.Equal(t,"127.0.0.1:16389",c.IDGenerator.Providers.Redis.Addr)
 require.NoError(t,c.Validate())
}
