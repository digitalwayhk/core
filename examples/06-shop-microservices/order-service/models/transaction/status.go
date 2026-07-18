// 本文件定义当前服务交易事实、Outbox、Inbox 或投影模型能力。
package transaction

const (
	OrderStatusNormal = iota
	OrderStatusCancelling
	OrderStatusCancelled
)

const (
	PaymentStatusUnpaid = iota
	PaymentStatusProcessing
	PaymentStatusPaid
	PaymentStatusFailed
	PaymentStatusRefunding
	PaymentStatusRefunded
)
