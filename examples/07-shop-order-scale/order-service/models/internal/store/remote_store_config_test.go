// 本文件验证07显式驱动协议配置，不改变框架全局默认。
package store

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// TestRemoteMySQLProtocolConfig 验证07显式utf8mb4插值，完整DSN保留显式选择。
func TestRemoteMySQLProtocolConfig(t *testing.T) {
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_DSN", "")
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_MAX_IDLE", "")
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_MAX_OPEN", "")
	config := remoteMySQLConfig()
	require.True(t, config.InterpolateParams)
	require.Equal(t, "utf8mb4", config.Charset)
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_DSN", "user:secret@tcp(127.0.0.1:3306)/orders?charset=utf8mb4")
	require.False(t, remoteMySQLConfig().InterpolateParams)
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_DSN", "user:secret@tcp(127.0.0.1:3306)/orders?charset=utf8mb4&interpolateParams=true")
	require.True(t, remoteMySQLConfig().InterpolateParams)
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_DSN", "secret-invalid-dsn")
	require.PanicsWithValue(t, "无效的 SHOP_ORDER_REMOTE_MYSQL_DSN 配置", func() { remoteMySQLConfig() })
}
