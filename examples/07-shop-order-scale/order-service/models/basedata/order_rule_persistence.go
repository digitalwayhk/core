// Package basedata 提供 07 订单服务基础资料的远程权威库访问能力。
package basedata

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// GetEnabledOrderRuleWith 从远程权威库读取启用的订单规则。
func GetEnabledOrderRuleWith(action persistencetypes.IDataAction, ruleCode string) (*OrderRule, error) {
	var items []*OrderRule
	query := store.NewSearch(NewOrderRule(), 1)
	query.AddWhereN("RuleCode", strings.TrimSpace(ruleCode))
	query.AddWhereN("Enabled", true)
	if err := action.Load(query, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("订单规则不存在")
	}
	return items[0], nil
}

// SaveOrderRuleWith 在远程权威库新增或更新订单规则。
func SaveOrderRuleWith(action persistencetypes.IDataAction, rule *OrderRule) error {
	if rule == nil {
		return errors.New("订单规则不能为空")
	}
	existing, err := findOrderRuleWith(action, rule.RuleCode)
	if err != nil {
		return rule.InsertWith(action)
	}
	rule.ID = existing.ID
	return rule.UpdateWith(action)
}

func findOrderRuleWith(action persistencetypes.IDataAction, ruleCode string) (*OrderRule, error) {
	var items []*OrderRule
	query := store.NewSearch(NewOrderRule(), 1)
	query.AddWhereN("RuleCode", strings.TrimSpace(ruleCode))
	if err := action.Load(query, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("订单规则不存在")
	}
	return items[0], nil
}
