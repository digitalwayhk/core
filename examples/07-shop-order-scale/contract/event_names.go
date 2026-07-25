// Package contract 定义 07 订单水平扩展示例的事件主题和事件类型契约。
package contract

// EventSchemaVersion 是 07 跨服务事件载荷的 schema 版本。
const EventSchemaVersion = 1

const (
	// SubjectOrderChanged 聚合订单接收、同步、创建、状态和支付变化事件。
	SubjectOrderChanged = "shop.order.changed"

	// SubjectOrderRuleChanged 表示订单规则配置变化事件主题。
	SubjectOrderRuleChanged = "shop.order_rule.changed"

	// SubjectSupplierChanged 表示供应商资料变化事件主题。
	SubjectSupplierChanged = "shop.supplier.changed"

	// SubjectProductChanged 表示商品资料变化事件主题。
	SubjectProductChanged = "shop.product.changed"

	// SubjectPaymentTypeChanged 表示支付类型配置变化事件主题。
	SubjectPaymentTypeChanged = "shop.payment_type.changed"
)

const (
	// EventOrderAccepted 表示某个 order 副本已可靠接收订单请求。
	EventOrderAccepted = "OrderAccepted"

	// EventOrderSynced 表示本地 pending 订单已同步到远程 order 权威库。
	EventOrderSynced = "OrderSynced"

	// EventOrderCreated 表示远程 order 权威库已生成订单事实。
	EventOrderCreated = "OrderCreated"

	// EventOrderStatusChanged 表示订单状态已变化。
	EventOrderStatusChanged = "OrderStatusChanged"

	// EventPaymentChanged 表示订单支付状态已变化。
	EventPaymentChanged = "PaymentChanged"

	// EventOrderRuleChanged 表示订单规则配置已变化。
	EventOrderRuleChanged = "OrderRuleChanged"

	// EventSupplierChanged 表示供应商资料已变化。
	EventSupplierChanged = "SupplierChanged"

	// EventProductChanged 表示商品资料已变化。
	EventProductChanged = "ProductChanged"

	// EventPaymentTypeChanged 表示支付类型配置已变化。
	EventPaymentTypeChanged = "PaymentTypeChanged"
)
