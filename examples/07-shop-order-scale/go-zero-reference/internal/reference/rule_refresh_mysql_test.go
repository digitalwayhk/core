// 本文件验证原生对照使用同一权威规则，更新后不继续使用旧快照。
package reference

import (
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestNativeRuleReadsAuthority 验证已校验过请求的两个实例感知规则收紧及恢复。
func TestNativeRuleReadsAuthority(t *testing.T) {
	if os.Getenv("CORE_REFERENCE_TEST_MYSQL") != "1" {
		t.Skip("需要计时外准备的专用MySQL schema")
	}
	db, err := openDatabase("order", "")
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	defer pool.Close()
	rule := &OrderRule{RuleCode: "default", RuleName: "更新验证", MinQuantity: 1, MaxQuantity: 100, MaxOrderAmount: decimal.NewFromInt(99999), Enabled: true}
	rule.ID = uint(time.Now().UnixNano() >> 8)
	require.NoError(t, db.Omit("service_instance_id", "service_instance_ip").Create(rule).Error)
	first, second := &application{db: db}, &application{db: db}
	request := &OrderRequest{Quantity: 2, UnitPrice: decimal.NewFromInt(10)}
	for _, app := range []*application{first, second} {
		require.NoError(t, app.validateRule(request))
	}
	require.NoError(t, db.Model(rule).Update("min_quantity", 3).Error)
	for _, app := range []*application{first, second} {
		require.Error(t, app.validateRule(request))
	}
	require.NoError(t, db.Model(rule).Update("min_quantity", 1).Error)
	for _, app := range []*application{first, second} {
		require.NoError(t, app.validateRule(request))
	}
	require.NoError(t, pool.Close())
	for _, app := range []*application{first, second} {
		require.Error(t, app.validateRule(request), "权威库不可用时不能继续使用旧规则")
	}
}
