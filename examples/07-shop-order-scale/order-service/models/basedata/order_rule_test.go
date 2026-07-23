// 本文件验证 07 订单规则保存在共享远程权威库中，多个 order 实例读取结果一致。
package basedata_test

import (
	"strings"
	"testing"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestOrderRuleStoredInRemoteAuthority 验证修改 default 规则后任意实例读取同一份远程配置。
func TestOrderRuleStoredInRemoteAuthority(t *testing.T) {
	if err := models.EnsureStorage(); err != nil {
		if strings.Contains(err.Error(), "dial tcp") || strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("MySQL 权威库不可用，跳过订单规则远程权威库测试: %v", err)
		}
		require.NoError(t, err)
	}

	rule := models.NewOrderRule()
	rule.ID = 720001
	rule.RuleCode = "default"
	rule.RuleName = "默认规则"
	rule.MinQuantity = 3
	rule.MaxQuantity = 100
	rule.MaxOrderAmount = decimal.NewFromInt(99999)
	rule.Enabled = true
	rule.RuleRevision = 2
	rule.TraceID = "trace-rule-update"
	rule.ServiceName = "shop-order"

	require.NoError(t, models.RunRemoteTransaction(func(action models.DataAction) error {
		return models.SaveOrderRuleWith(action, rule)
	}))

	var fromA *models.OrderRule
	require.NoError(t, models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		fromA, err = models.GetEnabledOrderRuleWith(action, "default")
		return err
	}))

	var fromB *models.OrderRule
	require.NoError(t, models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		fromB, err = models.GetEnabledOrderRuleWith(action, "default")
		return err
	}))

	require.Equal(t, fromA.ID, fromB.ID)
	require.Equal(t, 3, fromB.MinQuantity)
	require.Equal(t, 2, fromB.RuleRevision)
}
