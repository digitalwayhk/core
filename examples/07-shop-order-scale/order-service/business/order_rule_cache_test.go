// 本文件验证 07 订单服务规则缓存从共享远程权威库读取并按事件失效。
package business

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestOrderRuleCacheInvalidatesRemoteAuthorityRule 验证规则变更后失效缓存可读取最新规则。
func TestOrderRuleCacheInvalidatesRemoteAuthorityRule(t *testing.T) {
	require.NoError(t, models.EnsureStorage())
	ruleID := uint(750000 + time.Now().UnixNano()%1000000)

	cache := NewOrderRuleCache("default")
	require.NoError(t, saveRule(ruleID, 3, 1))

	first, err := cache.Get()
	require.NoError(t, err)
	require.Equal(t, 3, first.MinQuantity)

	require.NoError(t, saveRule(ruleID, 5, 2))
	cached, err := cache.Get()
	require.NoError(t, err)
	require.Equal(t, 3, cached.MinQuantity)

	cache.Invalidate()
	latest, err := cache.Get()
	require.NoError(t, err)
	require.Equal(t, 5, latest.MinQuantity)

	require.Error(t, cache.ValidateQuantityAndAmount(4, decimal.NewFromInt(40)))
	require.NoError(t, cache.ValidateQuantityAndAmount(5, decimal.NewFromInt(50)))
}

func saveRule(id uint, minQuantity int, revision int) error {
	rule := models.NewOrderRule()
	rule.ID = id
	rule.RuleCode = "default"
	rule.RuleName = "默认规则"
	rule.MinQuantity = minQuantity
	rule.MaxQuantity = 100
	rule.MaxOrderAmount = decimal.NewFromInt(99999)
	rule.Enabled = true
	rule.RuleRevision = revision
	rule.TraceID = "trace-rule-cache"
	return models.RunRemoteTransaction(func(action models.DataAction) error {
		return models.SaveOrderRuleWith(action, rule)
	})
}
