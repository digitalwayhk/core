// Package shoporderscale 提供 07 单进程 UAT 的 MySQL 权威库可用性检查。
package shoporderscale

import (
	"strings"
	"testing"

	ordermodels "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/stretchr/testify/require"
)

// requireOrderMySQL 确保依赖 MySQL 远程权威库的测试只在真实权威库可用时运行。
func requireOrderMySQL(t testing.TB) {
	t.Helper()
	if err := ordermodels.EnsureStorage(); err != nil {
		if strings.Contains(err.Error(), "dial tcp") || strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("MySQL 权威库不可用，跳过 07 远程权威库 UAT: %v", err)
		}
		require.NoError(t, err)
	}
}
