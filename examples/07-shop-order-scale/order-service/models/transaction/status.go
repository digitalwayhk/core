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

const (
	// PendingStatusAccepted 表示本地 pending 已可靠保存但尚未同步。
	PendingStatusAccepted = "accepted"

	// PendingStatusSynced 表示本地 pending 已完成远程同步。
	PendingStatusSynced = "synced"

	// PendingStatusFailed 表示本地 pending 最近一次同步失败。
	PendingStatusFailed = "failed"
)
