// Package business 实现 07 订单服务的共享权威规则读取与校验。
package business

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/shopspring/decimal"
)

// OrderRuleCache 保留示例原调用面；内部权威服务每次读取共享规则，不保留旧快照。
type OrderRuleCache struct {
	ruleCode string
	loader   func(ruleCode string) (*models.OrderRule, error)
}

// NewOrderRuleCache 创建指定规则编码的权威读取器，名称保留兼容。
func NewOrderRuleCache(ruleCode string) *OrderRuleCache {
	if strings.TrimSpace(ruleCode) == "" {
		ruleCode = "default"
	}
	return &OrderRuleCache{
		ruleCode: strings.TrimSpace(ruleCode),
		loader:   loadEnabledOrderRule,
	}
}

// Get 每次读取共享权威规则，管理修改完成后的新读取不依赖失效通知或进程重启。
func (c *OrderRuleCache) Get() (*models.OrderRule, error) {
	rule, err := c.loader(c.ruleCode)
	if errors.Is(err, models.ErrOrderRuleNotFound) {
		// 权威库尚未配置时使用内建默认值，但不缓存该回退值。
		// 这样管理员随后新增规则，无需重启服务即可在下一次下单时生效。
		return models.NewOrderRule(), nil
	} else if err != nil {
		return nil, err
	}
	item := *rule
	return &item, nil
}

func loadEnabledOrderRule(ruleCode string) (*models.OrderRule, error) {
	var rule *models.OrderRule
	err := models.RunRemoteRead(func(action models.DataAction) error {
		var err error
		rule, err = models.GetEnabledOrderRuleWith(action, ruleCode)
		return err
	})
	return rule, err
}

// Invalidate 保留旧调用兼容；权威读取器已不保存快照，无需手工失效。
// Deprecated: 规则每次从共享权威库读取。
func (c *OrderRuleCache) Invalidate() {}

// ValidateQuantityAndAmount 按当前共享订单规则校验数量和总金额。
func (c *OrderRuleCache) ValidateQuantityAndAmount(quantity int, totalAmount decimal.Decimal) error {
	rule, err := c.Get()
	if err != nil {
		return err
	}
	if !rule.Enabled {
		return errors.New("订单规则未启用")
	}
	if quantity < rule.MinQuantity {
		return errors.New("订单数量小于最小下单数量")
	}
	if quantity > rule.MaxQuantity {
		return errors.New("订单数量超过最大下单数量")
	}
	if totalAmount.GreaterThan(rule.MaxOrderAmount) {
		return errors.New("订单金额超过最大下单金额")
	}
	return nil
}

func serviceNameOrDefault(serviceName string) string {
	if strings.TrimSpace(serviceName) == "" {
		return "shop-order"
	}
	return strings.TrimSpace(serviceName)
}
