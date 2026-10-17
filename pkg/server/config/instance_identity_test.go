// 本文件验证稳定实例模式必须显式配置持久状态与独立编号权威。
package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStableIdentityConfig 验证 stable 模式不继承发现降级，不接受不支持的 Provider。
func TestStableIdentityConfig(t *testing.T) {
	c := NewServiceDefaultConfig("identity-test", 18080)
	c.Cluster.Mode = "off"
	c.Cluster.IdentityMode = "stable"
	c.Cluster.InstanceID = "orders-a"
	c.Cluster.IdentityFile = filepath.Join(t.TempDir(), "identity.json")
	require.Error(t, c.Validate())
	c.IDGenerator = &IDGeneratorConfig{Provider: "local", Namespace: "test", Epoch: "epoch-a", LocalDevelopment: true, LocalPath: filepath.Join(t.TempDir(), "authority.json")}
	require.NoError(t, c.Validate())
	c.IDGenerator.LocalDevelopment = false
	require.Error(t, c.Validate())
	c.IDGenerator.LocalDevelopment = true
	c.Cluster.IdentityFile = "relative.json"
	require.Error(t, c.Validate())
	c.Cluster.IdentityFile = filepath.Join(t.TempDir(), "identity.json")
	c.Cluster.Mode = "on"
	c.Cluster.Provider = "consul"
	c.Cluster.Providers.Consul.Address = "127.0.0.1:8500"
	require.Error(t, c.Validate())
}

// TestGeneratorConfigDoesNotFallback 验证编号连接配置必须来自独立字段。
func TestGeneratorConfigDoesNotFallback(t *testing.T) {
	c := NewServiceDefaultConfig("identity-test", 18080)
	c.Cluster.Mode = "off"
	c.Cluster.IdentityMode = "stable"
	c.Cluster.IdentityFile = filepath.Join(t.TempDir(), "identity.json")
	c.Cluster.Provider = "redis"
	c.Cluster.Providers.Redis.Addr = "127.0.0.1:6379"
	c.IDGenerator = &IDGeneratorConfig{Provider: "redis", Namespace: "test", Epoch: "epoch-a"}
	require.Error(t, c.Validate())
	c.IDGenerator.Providers.Redis.Addr = "127.0.0.1:6379"
	require.NoError(t, c.Validate())
	c.IDGenerator.Provider = "consul"
	require.Error(t, c.Validate())
}
