// 本文件验证不同驱动/凭证/端点不能复用连接或表缓存身份。
package oltp

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// TestMySQLIdentityIncludesCredentials 验证同库不同账号/口令不会命中旧池。
func TestMySQLIdentityIncludesCredentials(t *testing.T) {
	a := NewMySQL(&Config{Host: "one", Port: 3306, Database: "same", Username: "u", Password: "first"})
	b := NewMySQL(&Config{Host: "one", Port: 3306, Database: "same", Username: "u", Password: "second"})
	a.Name = "same"
	b.Name = "same"
	require.NotEqual(t, a.getConnectionKey(), b.getConnectionKey())
	require.NotEqual(t, a.GetSyncPoolKey(nil), b.GetSyncPoolKey(nil))
	require.NotContains(t, a.getConnectionKey(), "first")
}

// TestMySQLIdentityStableAcrossDefaults 验证旧构造器首次构造DSN不会改变连接身份。
func TestMySQLIdentityStableAcrossDefaults(t *testing.T) {
	c := &Config{Host: "one", Port: 3306, Database: "same", Username: "u", Password: "p"}
	a := NewMySQL(c)
	before := a.GetSyncPoolKey(nil)
	_ = a.buildDSN()
	_ = a.buildDSNWithDB("same")
	_ = c.MysqlDSN()
	require.Equal(t, before, a.GetSyncPoolKey(nil))
}
