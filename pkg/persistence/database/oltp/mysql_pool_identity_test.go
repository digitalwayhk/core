// 本文件验证同地址/库的不同认证不得借用已有共享连接。
package oltp

import (
	"context"
	"github.com/stretchr/testify/require"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMySQLPoolIdentitySeparatesCredentials 验证认证、字符集及数据库隔离，同时保留同配置复用。
func TestMySQLPoolIdentitySeparatesCredentials(t *testing.T) {
	config := Config{Host: "127.0.0.1", Port: 3306, Username: "account_a", Password: "secret_password_marker", Database: "orders"}
	base := NewMySQL(&config)
	first := base.getConnectionKey()
	require.Equal(t, first, base.getConnectionKey())
	require.NotContains(t, first, config.Password)
	for _, change := range []func(*Config){func(c *Config) { c.Username = "account_b" }, func(c *Config) { c.Password = "another_password" }, func(c *Config) { c.Charset = "latin1" }, func(c *Config) { c.Loc = "UTC" }} {
		changed := config
		change(&changed)
		require.NotEqual(t, first, NewMySQL(&changed).getConnectionKey())
		require.NotEqual(t, base.GetSyncPoolKey(nil), NewMySQL(&changed).GetSyncPoolKey(nil))
	}
	identical := config
	identical.Charset = "utf8mb4"
	identical.Loc = "Local"
	identical.ParseTime = true
	identical.MaxIdleConns = 20
	identical.MaxOpenConns = 20
	identical.IsLog = true
	require.Equal(t, first, NewMySQL(&identical).getConnectionKey())
	require.Equal(t, base.GetSyncPoolKey(nil), NewMySQL(&identical).GetSyncPoolKey(nil))
	other := NewMySQL(&config)
	other.Name = "other_orders"
	require.NotEqual(t, first, other.getConnectionKey())
}

// TestMySQLPoolIdentityRejectsWrongCredentials 验证真实MySQL错误凭据不能借用首个账号已认证的连接池。
func TestMySQLPoolIdentityRejectsWrongCredentials(t *testing.T) {
	address := os.Getenv("CORE_POOL_MYSQL_ADDR")
	if address == "" {
		t.Skip("需要本轮独占CORE_POOL_MYSQL_ADDR")
	}
	host, port, err := net.SplitHostPort(address)
	require.NoError(t, err)
	number, err := strconv.Atoi(port)
	require.NoError(t, err)
	config := Config{Host: host, Port: number, Username: "root", Password: os.Getenv("CORE_POOL_MYSQL_PASSWORD"), Charset: "utf8mb4", Loc: "Local", ParseTime: true, MaxOpenConns: 2, MaxIdleConns: 2}
	base := NewMySQL(&config)
	db, err := base.GetDB()
	require.NoError(t, err)
	t.Cleanup(func() {
		connManager.Remove(base.getConnectionKey())
		pool, e := db.DB()
		if e == nil {
			_ = pool.Close()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var authenticated string
	require.NoError(t, db.WithContext(ctx).Raw("SELECT CURRENT_USER()").Scan(&authenticated).Error)
	require.True(t, strings.HasPrefix(authenticated, "root@"))
	same := NewMySQL(&config)
	reused, err := same.GetDB()
	require.NoError(t, err)
	require.Same(t, db, reused)
	wrong := config
	wrong.Username = "core_queue_audit_nonexistent"
	wrong.Password = "not-a-valid-password"
	_, err = NewMySQL(&wrong).GetDB()
	require.Error(t, err, "错误凭据不得借用已认证连接")
}
