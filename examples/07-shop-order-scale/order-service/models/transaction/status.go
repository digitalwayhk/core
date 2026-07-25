// Package transaction 定义 07 订单服务交易事实使用的状态常量。
package transaction

const (
	// OrderStatusAccepted 表示订单已被某个 order 实例本地可靠接收。
	OrderStatusAccepted = "accepted"

	// OrderStatusSynced 表示订单已同步到远程权威库。
	OrderStatusSynced = "synced"

	// OrderStatusCancelled 表示订单已撤销。
	OrderStatusCancelled = "cancelled"
)

const (
	// PaymentStatusUnpaid 表示订单尚未支付。
	PaymentStatusUnpaid = "unpaid"

	// PaymentStatusPaid 表示订单已支付。
	PaymentStatusPaid = "paid"
)
