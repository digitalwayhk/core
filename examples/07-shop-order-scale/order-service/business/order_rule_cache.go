// Package business 实现 07 订单服务共享规则的本地缓存能力。
package business

import (
	"errors"
	"strings"
	"sync"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/shopspring/decimal"
)

// OrderRuleCache 缓存共享远程权威库中的订单规则快照。
type OrderRuleCache struct {
	ruleCode string
	mu       sync.RWMutex
	cached   *models.OrderRule
	loader   func(ruleCode string) (*models.OrderRule, error)
}

// NewOrderRuleCache 创建指定规则编码的订单规则缓存。
func NewOrderRuleCache(ruleCode string) *OrderRuleCache {
	if strings.TrimSpace(ruleCode) == "" {
		ruleCode = "default"
	}
	return &OrderRuleCache{
		ruleCode: strings.TrimSpace(ruleCode),
		loader:   loadEnabledOrderRule,
	}
}

// Get 读取订单规则；缓存 miss 时访问共享远程权威库。
func (c *OrderRuleCache) Get() (*models.OrderRule, error) {
	c.mu.RLock()
	if c.cached != nil {
		item := *c.cached
		c.mu.RUnlock()
		return &item, nil
	}
	c.mu.RUnlock()
	rule, err := c.loader(c.ruleCode)
	if errors.Is(err, models.ErrOrderRuleNotFound) {
		// 权威库尚未配置时使用内建默认值，但不缓存该回退值。
		// 这样管理员随后新增规则，无需重启服务即可在下一次下单时生效。
		return models.NewOrderRule(), nil
	} else if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cached = rule
	c.mu.Unlock()
	item := *rule
	return &item, nil
}

func loadEnabledOrderRule(ruleCode string) (*models.OrderRule, error) {
	var rule *models.OrderRule
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		rule, err = models.GetEnabledOrderRuleWith(action, ruleCode)
		return err
	})
	return rule, err
}

// Invalidate 失效当前实例的订单规则缓存。
func (c *OrderRuleCache) Invalidate() {
	c.mu.Lock()
	c.cached = nil
	c.mu.Unlock()
}

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
