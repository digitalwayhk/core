// Package manage 是 07 订单服务后台管理 API 的兼容门面。
package manage

import (
	basedatamanage "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/basedata"
	transactionmanage "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/transaction"
)

type (
	// OrderRuleManage 是订单规则 Manage 别名。
	OrderRuleManage = basedatamanage.OrderRuleManage

	// PaymentTypeManage 是支付类型 Manage 别名。
	PaymentTypeManage = basedatamanage.PaymentTypeManage

	// OrderManage 是订单查询 Manage 别名。
	OrderManage = transactionmanage.OrderManage
)

var (
	// NewOrderRuleManage 创建订单规则 Manage。
	NewOrderRuleManage = basedatamanage.NewOrderRuleManage

	// NewPaymentTypeManage 创建支付类型 Manage。
	NewPaymentTypeManage = basedatamanage.NewPaymentTypeManage

	// NewOrderManage 创建订单查询 Manage。
	NewOrderManage = transactionmanage.NewOrderManage
)
