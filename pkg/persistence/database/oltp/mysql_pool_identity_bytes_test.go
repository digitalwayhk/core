// 本文件验证连接池身份按凭据和驱动配置原始字节隔离，不能将非法UTF8字节替换后混池。
package oltp

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// TestMySQLPoolIdentityPreservesCredentialBytes 验证JSON默认的非法UTF8替换不能导致两个不同凭据共用身份。
func TestMySQLPoolIdentityPreservesCredentialBytes(t *testing.T) {
	for _, field := range []string{"username", "password", "charset", "loc"} {
		t.Run(field, func(t *testing.T) {
			first := Config{Host: "localhost", Port: 3306, Username: "account", Password: "secret", Database: "orders"}
			second := first
			if field == "username" {
				first.Username = string([]byte{255})
				second.Username = string([]byte{254})
			} else if field == "charset" {
				first.Charset = string([]byte{255})
				second.Charset = string([]byte{254})
			} else if field == "loc" {
				first.Loc = string([]byte{255})
				second.Loc = string([]byte{254})
			} else {
				first.Password = string([]byte{255})
				second.Password = string([]byte{254})
			}
			require.NotEqual(t, NewMySQL(&first).getConnectionKey(), NewMySQL(&second).getConnectionKey(), "不同凭据原始字节必须隔离")
		})
	}
}

// TestMySQLPoolIdentityRejectsCredentialBoundaryAliases 验证无分隔符或NUL拼接的相同字节串不能混池。
func TestMySQLPoolIdentityRejectsCredentialBoundaryAliases(t *testing.T) {
	for _, pair := range [][4]string{{"ab", "c", "a", "bc"}, {"a\x00", "b", "a", "\x00b"}} {
		first := Config{Host: "localhost", Port: 3306, Username: pair[0], Password: pair[1]}
		second := first
		second.Username = pair[2]
		second.Password = pair[3]
		require.NotEqual(t, NewMySQL(&first).getConnectionKey(), NewMySQL(&second).getConnectionKey())
	}
}
