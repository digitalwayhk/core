// 本文件验证远端类型选择集中在 models，不要求业务/Manage 修改。
package store

import (
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/stretchr/testify/require"
	"testing"
)

// TestRemoteLegacyMySQLActionConfig 验证实际访问器保留旧DSN与07显式驱动协议选择。
func TestRemoteLegacyMySQLActionConfig(t *testing.T) {
	t.Setenv("SHOP_ORDER_REMOTE_DRIVER", "")
	for _, dsn := range []string{"", "user:secret@tcp(127.0.0.1:3306)/orders?charset=latin1&loc=UTC"} {
		t.Setenv("SHOP_ORDER_REMOTE_MYSQL_DSN", dsn)
		want := remoteMySQLConfig()
		action, err := newRemoteAction()
		require.NoError(t, err)
		mysql, ok := action.(*oltp.MySQL)
		require.True(t, ok)
		require.Equal(t, want, mysql.GetConfig())
	}
}

// TestRemoteStorageConfig 验证显式 PostgreSQL 配置与旧 MySQL 配置并存。
func TestRemoteStorageConfig(t *testing.T) {
	t.Setenv("SHOP_ORDER_REMOTE_DRIVER", "postgres")
	t.Setenv("SHOP_ORDER_REMOTE_HOST", "127.0.0.1")
	t.Setenv("SHOP_ORDER_REMOTE_USER", "core_test")
	t.Setenv("SHOP_ORDER_REMOTE_DATABASE", "shared")
	c := remoteStorageConfig()
	require.Equal(t, "postgres", c.Driver)
	require.Equal(t, 5432, c.Port)
	require.Equal(t, "shared", c.Database)
	t.Setenv("SHOP_ORDER_REMOTE_DRIVER", "")
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_PORT", "15306")
	require.Equal(t, 15306, remoteStorageConfig().Port)
}
