// 本文件验证 Manage 控制面存储只由 server.json 配置。
package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestServerDefaultConfigOwnsManageStore 验证只有内置 server 服务生成控制面存储配置。
func TestServerDefaultConfigOwnsManageStore(t *testing.T) {
	server := NewServiceDefaultConfig("server", 18080)
	require.NotNil(t, server.ManageStore)
	require.Equal(t, ManageStoreDriverSQLite, server.ManageStore.Driver)
	require.Equal(t, "core_manage", server.ManageStore.Database)

	business := NewServiceDefaultConfig("orders", 18081)
	require.Nil(t, business.ManageStore)
	business.ManageStore = DefaultManageStoreConfig()
	require.ErrorContains(t, business.Validate(), "server")
}

// TestOldServerConfigDefaultsManageStore 验证旧 server.json 缺少字段时仍兼容 SQLite。
func TestOldServerConfigDefaultsManageStore(t *testing.T) {
	cfg := NewServiceDefaultConfig("server", 18080)
	cfg.ManageStore = nil

	cfg.ApplyDefaults()

	require.NotNil(t, cfg.ManageStore)
	require.Equal(t, ManageStoreDriverSQLite, cfg.ManageStore.Driver)
	require.Equal(t, "models", cfg.ManageStore.Database)
	require.NoError(t, cfg.Validate())
}

// TestManageStoreMySQLRequiresConnectionFields 验证 MySQL 模式缺少连接字段时 fail closed。
func TestManageStoreMySQLRequiresConnectionFields(t *testing.T) {
	cfg := DefaultManageStoreConfig()
	cfg.Driver = ManageStoreDriverMySQL

	require.ErrorContains(t, cfg.Validate(), "Host")

	cfg.Host = "mysql.internal"
	require.ErrorContains(t, cfg.Validate(), "Username")

	cfg.Username = "core"
	require.NoError(t, cfg.Validate())
}

// TestManageStoreRejectsUnknownDriverAndInvalidPool 验证驱动与连接池参数不得被默认容错。
func TestManageStoreRejectsUnknownDriverAndInvalidPool(t *testing.T) {
	cfg := DefaultManageStoreConfig()
	cfg.Driver = "postgres"
	require.ErrorContains(t, cfg.Validate(), "Driver")

	cfg = DefaultManageStoreConfig()
	cfg.MaxIdleConns = 51
	require.ErrorContains(t, cfg.Validate(), "MaxIdleConns")
}
