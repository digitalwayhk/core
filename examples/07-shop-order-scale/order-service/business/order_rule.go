// Package business 提供 07 订单服务订单规则业务能力。
package business

import "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"

// SaveOrderRule 保存共享订单规则配置。
func SaveOrderRule(item *models.OrderRule) (*models.OrderRule, error) {
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		return models.SaveOrderRuleWith(action, item)
	})
	return item, err
}
