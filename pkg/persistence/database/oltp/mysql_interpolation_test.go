// 本文件验证驱动插值的显式配置、默认兼容及不安全字符集拒绝。
package oltp

import (
	"bytes"
	"fmt"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	gomysql "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

// TestMySQLInterpolationConfiguration 验证显式解析、双路径DSN透传及非法值拒绝。
func TestMySQLInterpolationConfiguration(t *testing.T) {
	for _, enabled := range []string{"true", "false"} {
		t.Run(enabled, func(t *testing.T) {
			config := Config{}
			require.NoError(t, config.SetMysqlDSN("user:secret@tcp(127.0.0.1:3306)/orders?charset=utf8mb4&interpolateParams="+enabled))
			adapter := NewMySQL(&config)
			for _, dsn := range []string{config.MysqlDSN(), adapter.buildDSN(), adapter.buildDSNWithDB("orders")} {
				parsed, err := gomysql.ParseDSN(dsn)
				require.NoError(t, err)
				require.Equal(t, enabled == "true", parsed.InterpolateParams)
				require.True(t, parsed.ParseTime)
			}
		})
	}
	config := Config{}
	require.Error(t, config.SetMysqlDSN("user:secret@tcp(127.0.0.1:3306)/orders?interpolateParams=invalid"))
}

// TestMySQLInterpolationDefaultsAndPoolIsolation 验证零值关闭，协议不同的池保持隔离。
func TestMySQLInterpolationDefaultsAndPoolIsolation(t *testing.T) {
	config := Config{Host: "127.0.0.1", Port: 3306, Username: "user", Password: "secret", Charset: "utf8mb4"}
	baseline := NewMySQL(&config)
	require.NotContains(t, baseline.buildDSN(), "interpolateParams=true")
	alternate := Config{}
	require.NoError(t, alternate.SetMysqlDSN(config.MysqlDSN()+"&interpolateParams=true"))
	require.NotEqual(t, baseline.getConnectionKey(), NewMySQL(&alternate).getConnectionKey())
}

// TestMySQLInterpolationRejectsUnverifiedCharsetsBeforeDial 验证未验证字符集在建连前拒绝且不泄露凭据。
func TestMySQLInterpolationRejectsUnverifiedCharsetsBeforeDial(t *testing.T) {
	for _, charset := range []string{"gbk", "big5", "sjis", "utf8mb4,gbk", "latin1"} {
		config := Config{}
		require.NoError(t, config.SetMysqlDSN("user:secret@tcp(127.0.0.1:1)/orders?charset="+charset+"&interpolateParams=true"))
		_, err := NewMySQL(&config).GetDB()
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "utf8mb4"), "须在连接前拒绝未验证字符集")
		require.NotContains(t, err.Error(), "secret")
	}
}

// TestMySQLInterpolationRealParametersAndRollback 验证两种协议下危险字节、金额、业务写回滚和提交保持相同结果。
func TestMySQLInterpolationRealParametersAndRollback(t *testing.T) {
	base := newPoolRecoveryAdapter(t)
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			config := Config{}
			require.NoError(t, config.SetMysqlDSN(base.config.MysqlDSN()+fmt.Sprintf("&interpolateParams=%t", enabled)))
			adapter := NewMySQL(&config)
			adapter.Name = base.Name
			parsed, parseErr := gomysql.ParseDSN(adapter.buildDSN())
			require.NoError(t, parseErr)
			require.Equal(t, enabled, parsed.InterpolateParams)
			db, err := adapter.GetDB()
			require.NoError(t, err)
			t.Cleanup(func() { connManager.Remove(adapter.getConnectionKey()) })
			value := "中文😀'; SELECT 123; --" + string([]byte{92, 34, 0, 26})
			binary := []byte{0, 26, 39, 92, 255}
			amount := "12345678901234567890.12345678"
			var text, money string
			var data []byte
			var absent any
			var integer int64
			require.NoError(t, db.Raw("SELECT ?, ?, ?, CAST(? AS DECIMAL(30,8)), ?", value, binary, nil, amount, int64(-9223372036854775807)).Row().Scan(&text, &data, &absent, &money, &integer))
			require.Equal(t, value, text)
			require.True(t, bytes.Equal(binary, data))
			require.Nil(t, absent)
			require.Equal(t, amount, money)
			require.Equal(t, int64(-9223372036854775807), integer)
			tx := adapter.Clone().(*MySQL)
			require.NoError(t, tx.Transaction())
			defer tx.Rollback()
			require.NoError(t, tx.Insert(&poolRecoveryFact{Value: value}))
			require.NoError(t, tx.Rollback())
			var facts []*poolRecoveryFact
			require.NoError(t, adapter.Load(&types.SearchItem{Model: &poolRecoveryFact{}, Page: 1, Size: 10, SkipCount: true}, &facts))
			require.Empty(t, facts)
			require.NoError(t, tx.Transaction())
			require.NoError(t, tx.Insert(&poolRecoveryFact{Value: value}))
			require.NoError(t, tx.Commit())
			require.NoError(t, adapter.Load(&types.SearchItem{Model: &poolRecoveryFact{}, Page: 1, Size: 10, SkipCount: true}, &facts))
			require.Len(t, facts, 1)
			require.Equal(t, value, facts[0].Value)
			require.NoError(t, adapter.Delete(facts[0]))
		})
	}
}
