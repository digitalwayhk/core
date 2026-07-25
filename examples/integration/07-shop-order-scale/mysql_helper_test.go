// Package shoporderscale 提供 07 单进程 UAT 的 MySQL 权威库可用性检查。
package shoporderscale

import (
	"os"
	"testing"

	ordermodels "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/stretchr/testify/require"
)

// requireOrderMySQL 确保依赖 MySQL 远程权威库的测试只在真实权威库可用时运行。
func requireOrderMySQL(t testing.TB) {
	t.Helper()
	if os.Getenv("CORE_TEST_MYSQL") != "1" {
		t.Skip("设置 CORE_TEST_MYSQL=1 后运行 07 远程权威库 UAT")
	}
	require.NoError(t, ordermodels.EnsureStorage())
}
