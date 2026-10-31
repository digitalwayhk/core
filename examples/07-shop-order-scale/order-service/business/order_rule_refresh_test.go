// 本文件验证权威规则变化和读取失败无需手工失效即可被既有校验器感知。
package business

import (
	"errors"
	"testing"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestOrderRuleReadersSeeChanges 验证两个已使用的实例随权威规则收紧、恢复而改变判断。
func TestOrderRuleReadersSeeChanges(t *testing.T) {
	current := models.NewOrderRule()
	first, second := NewOrderRuleCache("default"), NewOrderRuleCache("default")
	for _, reader := range []*OrderRuleCache{first, second} {
		reader.loader = func(string) (*models.OrderRule, error) { copy := *current; return &copy, nil }
		require.NoError(t, reader.ValidateQuantityAndAmount(2, decimal.NewFromInt(20)))
	}
	current.MinQuantity = 3
	for _, reader := range []*OrderRuleCache{first, second} {
		require.EqualError(t, reader.ValidateQuantityAndAmount(2, decimal.NewFromInt(20)), "订单数量小于最小下单数量")
	}
	current.MinQuantity = 1
	for _, reader := range []*OrderRuleCache{first, second} {
		require.NoError(t, reader.ValidateQuantityAndAmount(2, decimal.NewFromInt(20)))
	}
}

// TestOrderRuleReaderDoesNotHideAuthorityFailure 验证已成功读取后数据库故障也不能继续使用旧规则。
func TestOrderRuleReaderDoesNotHideAuthorityFailure(t *testing.T) {
	reader := NewOrderRuleCache("default")
	var readErr error
	reader.loader = func(string) (*models.OrderRule, error) { return models.NewOrderRule(), readErr }
	_, err := reader.Get()
	require.NoError(t, err)
	readErr = errors.New("权威规则读取失败")
	_, err = reader.Get()
	require.ErrorIs(t, err, readErr)
}
