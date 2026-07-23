package models

// OrderStatus 表示订单自身的生命周期，不与支付结果混用。
type OrderStatus int

const (
	OrderStatusNormal OrderStatus = iota
	OrderStatusCancelling
	OrderStatusCancelled
)

// String 返回管理界面和 DTO 使用的稳定中文名称。
func (status OrderStatus) String() string {
	switch status {
	case OrderStatusNormal:
		return "正常"
	case OrderStatusCancelling:
		return "撤销处理中"
	case OrderStatusCancelled:
		return "已撤销"
	default:
		return "未知"
	}
}

// PaymentStatus 表示订单当前支付和退款阶段。
type PaymentStatus int

const (
	PaymentStatusUnpaid PaymentStatus = iota
	PaymentStatusPending
	PaymentStatusPaid
	PaymentStatusFailed
	PaymentStatusRefunding
	PaymentStatusRefunded
)

// String 返回管理界面和 DTO 使用的稳定中文名称。
func (status PaymentStatus) String() string {
	switch status {
	case PaymentStatusUnpaid:
		return "未支付"
	case PaymentStatusPending:
		return "支付中"
	case PaymentStatusPaid:
		return "已支付"
	case PaymentStatusFailed:
		return "支付失败"
	case PaymentStatusRefunding:
		return "退款中"
	case PaymentStatusRefunded:
		return "已退款"
	default:
		return "未知"
	}
}
