// 本文件验证 07 订单服务每次读取共享权威规则，无需测试手工失效。
package business

import (
	"os"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestOrderRuleCacheInvalidatesRemoteAuthorityRule 验证两个既有读取器无需手工失效即可读取更新。
func TestOrderRuleCacheInvalidatesRemoteAuthorityRule(t *testing.T) {
	if os.Getenv("CORE_TEST_MYSQL") != "1" {
		t.Skip("设置 CORE_TEST_MYSQL=1 后运行规则缓存 MySQL 集成测试")
	}
	require.NoError(t, models.EnsureStorage())
	ruleID := uint(750000 + time.Now().UnixNano()%1000000)

	cache := NewOrderRuleCache("default")
	second := NewOrderRuleCache("default")
	require.NoError(t, saveRule(ruleID, 3, 1))

	first, err := cache.Get()
	require.NoError(t, err)
	require.Equal(t, 3, first.MinQuantity)
	_, err = second.Get()
	require.NoError(t, err)

	require.NoError(t, saveRule(ruleID, 5, 2))
	latest, err := cache.Get()
	require.NoError(t, err)
	require.Equal(t, 5, latest.MinQuantity)
	latest, err = second.Get()
	require.NoError(t, err)
	require.Equal(t, 5, latest.MinQuantity)

	require.Error(t, cache.ValidateQuantityAndAmount(4, decimal.NewFromInt(40)))
	require.NoError(t, cache.ValidateQuantityAndAmount(5, decimal.NewFromInt(50)))
}

func TestOrderRuleCacheUsesBuiltInDefaultWhenAuthorityHasNoRule(t *testing.T) {
	cache := NewOrderRuleCache("default")
	loads := 0
	cache.loader = func(string) (*models.OrderRule, error) {
		loads++
		if loads == 1 {
			return nil, models.ErrOrderRuleNotFound
		}
		configured := models.NewOrderRule()
		configured.MaxQuantity = 200
		return configured, nil
	}

	rule, err := cache.Get()

	require.NoError(t, err)
	require.Equal(t, "default", rule.RuleCode)
	require.Equal(t, 1, rule.MinQuantity)
	require.Equal(t, 100, rule.MaxQuantity)
	require.True(t, rule.Enabled)
	require.NoError(t, cache.ValidateQuantityAndAmount(130, decimal.NewFromInt(910)))
	require.Equal(t, 2, loads)
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
