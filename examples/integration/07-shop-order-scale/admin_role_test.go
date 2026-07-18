// Package shoporderscale 验证 07 单进程管理员角色的共享规则闭环。
package shoporderscale

import (
	"testing"
	"time"

	orderbusiness "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	ordermodels "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestUATAdminOrderRuleSharedAuthority 验证管理员修改共享规则后缓存失效读取最新配置。
func TestUATAdminOrderRuleSharedAuthority(t *testing.T) {
	requireOrderMySQL(t)
	rule := ordermodels.NewOrderRule()
	rule.ID = uint(820000 + time.Now().UnixNano()%1000000)
	rule.RuleCode = "default"
	rule.RuleName = "默认规则"
	rule.MinQuantity = 4
	rule.MaxQuantity = 100
	rule.MaxOrderAmount = decimal.NewFromInt(99999)
	rule.Enabled = true
	rule.RuleRevision = 40
	_, err := orderbusiness.SaveOrderRule(rule)
	require.NoError(t, err)

	cache := orderbusiness.NewOrderRuleCache("default")
	require.Error(t, cache.ValidateQuantityAndAmount(3, decimal.NewFromInt(36)))
	require.NoError(t, cache.ValidateQuantityAndAmount(4, decimal.NewFromInt(48)))
}
